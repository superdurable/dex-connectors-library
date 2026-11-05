# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Help Scout Queries and Mutations with Go-compatible provider outcomes."""

import ipaddress
import re
from dataclasses import replace
from datetime import timedelta
from email.utils import parseaddr
from typing import Any
from urllib.parse import urlsplit

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions
from .client import Client, Exchange
from .decode import (
    array_value,
    conversation,
    customer,
    int_value,
    object_value,
    parse_json,
    parse_timestamp,
    thread,
)
from .models import (
    Conversation,
    ConversationDetails,
    ConversationReply,
    CustomerMatches,
    FindCustomerByEmailInput,
    GetConversationInput,
    ReplyToConversationInput,
    SearchConversationsInput,
    SearchConversationsOutput,
    UpdateConversationInput,
    UpdateConversationOutput,
)


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "helpscout", operation, message)


def _zero_conversation() -> Conversation:
    return Conversation(status="")


def _zero_details() -> ConversationDetails:
    return ConversationDetails(_zero_conversation())


def _zero_update() -> UpdateConversationOutput:
    return UpdateConversationOutput(_zero_conversation())


def _query_exchange(
    client: Client,
    call: Call,
    operation: str,
    exchange: Exchange,
    value: Any,
    object_id: int,
    rejected: str,
    not_found: str = "",
) -> QueryAttempt[Any] | None:
    if exchange.error == "notBuilt":
        return QueryAttempt.branch_result(
            "defect",
            value,
            Receipt(),
            _failure(operation, FailureKind.LOCAL_DEFECT, "Help Scout request could not be built"),
        )
    if exchange.error in ("tooLarge", "malformed"):
        kind = FailureKind.RESPONSE_TOO_LARGE if exchange.error == "tooLarge" else FailureKind.PROTOCOL
        message = (
            "Help Scout response exceeds the configured maxResponseBytes"
            if exchange.error == "tooLarge"
            else "Help Scout returned a malformed response"
        )
        return QueryAttempt.branch_result(
            "invalidResponse", value, client.receipt(call, object_id), _failure(operation, kind, message)
        )
    if exchange.error:
        return QueryAttempt.retry(
            _failure(operation, FailureKind.TRANSPORT, "Help Scout could not be reached")
        )
    if 200 <= exchange.status < 300:
        return None
    retry, failure, delay, request_id = client.classify_failure(operation, exchange)
    if retry:
        return QueryAttempt.retry(failure, delay)
    branch = not_found if exchange.status in (404, 410) and not_found else rejected
    return QueryAttempt.branch_result(branch, value, client.receipt(call, object_id, request_id), failure)


def _mutation_exchange(
    client: Client,
    call: Call,
    operation: str,
    exchange: Exchange,
    value: Any,
    object_id: int,
    rejected: str,
    not_found: str = "",
) -> MutationAttempt[Any] | None:
    if exchange.error == "notBuilt":
        return MutationAttempt.branch_result(
            "defect",
            value,
            Receipt(),
            _failure(operation, FailureKind.LOCAL_DEFECT, "Help Scout request could not be built"),
        )
    if exchange.error in ("tooLarge", "malformed"):
        kind = FailureKind.RESPONSE_TOO_LARGE if exchange.error == "tooLarge" else FailureKind.PROTOCOL
        message = (
            "Help Scout response exceeds the configured maxResponseBytes"
            if exchange.error == "tooLarge"
            else "Help Scout returned a malformed response"
        )
        return MutationAttempt.branch_result(
            "invalidResponse", value, client.receipt(call, object_id), _failure(operation, kind, message)
        )
    if exchange.error:
        return MutationAttempt.retry(
            _failure(operation, FailureKind.TRANSPORT, "Help Scout could not be reached")
        )
    if 200 <= exchange.status < 300:
        return None
    retry, failure, delay, request_id = client.classify_failure(operation, exchange)
    if retry:
        return MutationAttempt.retry(failure, delay)
    branch = not_found if exchange.status in (404, 410) and not_found else rejected
    return MutationAttempt.branch_result(branch, value, client.receipt(call, object_id, request_id), failure)


def _credential_query(operation: str, error: str, value: Any) -> QueryAttempt[Any]:
    if error == "retry":
        return QueryAttempt.retry(
            _failure(
                operation,
                FailureKind.AVAILABILITY,
                "the Help Scout access token could not be loaded or obtained yet",
            )
        )
    return QueryAttempt.branch_result(
        "defect",
        value,
        Receipt(),
        _failure(
            operation,
            FailureKind.AUTHENTICATION,
            _credential_message(error),
        ),
    )


def _credential_mutation(operation: str, error: str, value: Any) -> MutationAttempt[Any]:
    if error == "retry":
        return MutationAttempt.retry(
            _failure(
                operation,
                FailureKind.AVAILABILITY,
                "the Help Scout access token could not be loaded or obtained yet",
            )
        )
    return MutationAttempt.branch_result(
        "defect",
        value,
        Receipt(),
        _failure(
            operation,
            FailureKind.AUTHENTICATION,
            _credential_message(error),
        ),
    )


def _credential_message(error: str) -> str:
    if error == "unusable":
        return "the Help Scout access token is blank or contains characters a header cannot carry"
    return (
        "Help Scout rejected the App ID or App Secret; save the app's current credentials "
        "in Dex Web Connections"
    )


def _bare_email(value: str) -> bool:
    if not value or any(character in value for character in " \"'()<>,;\\"):
        return False
    local, separator, domain = value.rpartition("@")
    if (
        not separator
        or not local
        or not domain
        or local.startswith(".")
        or local.endswith(".")
        or ".." in local
    ):
        return False
    if domain.startswith("[") and domain.endswith("]"):
        try:
            ipaddress.ip_address(domain[1:-1])
            return bool(re.fullmatch(r"[^@\s]+", local))
        except ValueError:
            return False
    name, address = parseaddr(value)
    if name or address != value:
        return False
    return bool(re.fullmatch(r"[\w-]+(?:\.[\w-]+)*", domain, re.UNICODE))


def _status(value: str, *, allow_all: bool = False) -> bool:
    return value in (
        ("active", "pending", "closed", "spam", "all")
        if allow_all
        else ("active", "pending", "closed", "spam")
    )


def _tags(field: str, values: tuple[str, ...]) -> None:
    seen: set[str] = set()
    for value in values:
        lowered = value.lower()
        if (
            not value
            or value.strip() != value
            or len(value.encode()) > 100
            or any(character in value for character in ',"()\\')
            or any(ord(character) < 32 or ord(character) == 127 for character in value)
            or lowered in seen
        ):
            raise ValueError(f"{field} holds an invalid or duplicate tag")
        seen.add(lowered)


def _search_query(input: SearchConversationsInput) -> dict[str, str]:
    if type(input.page) is not int or input.page < 0 or input.page > 10000:
        raise ValueError("page must be from 1 through 10000, or zero for page 1")
    if type(input.mailbox_id) is not int or input.mailbox_id < 0:
        raise ValueError("mailboxId must be a positive Help Scout inbox ID, or zero for every inbox")
    query = {"page": str(input.page or 1)}
    if input.mailbox_id:
        query["mailbox"] = str(input.mailbox_id)
    if input.status:
        if not _status(input.status, allow_all=True):
            raise ValueError("status is invalid")
        query["status"] = input.status
    if input.tag:
        _tags("tag", (input.tag,))
        query["tag"] = input.tag
    if input.customer_email:
        if not _bare_email(input.customer_email):
            raise ValueError("customerEmail must be one bare email address such as jane@example.com")
        query["query"] = f'(email:"{input.customer_email}")'
    if input.modified_since:
        instant = parse_timestamp(input.modified_since)
        query["modifiedSince"] = instant.strftime("%Y-%m-%dT%H:%M:%SZ")
    return query


class SearchConversationsOperation:
    """Search one page of conversations."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated search definition."""
        return definitions.SEARCH_CONVERSATIONS_DEFINITION

    async def invoke(
        self, call: Call, input: SearchConversationsInput
    ) -> QueryAttempt[SearchConversationsOutput]:
        """Validate filters and read one bounded page."""
        empty = SearchConversationsOutput()
        try:
            query = _search_query(input)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                _failure("searchConversations", FailureKind.VALIDATION, str(error)),
            )
        token, resolve_error = await self.client.resolve_token(call)
        if resolve_error:
            return _credential_query("searchConversations", resolve_error, empty)
        exchange = await self.client.exchange(call, token, "GET", "/v2/conversations", query)
        terminal = _query_exchange(
            self.client, call, "searchConversations", exchange, empty, 0, "providerRejected"
        )
        if terminal:
            return terminal
        try:
            page = parse_json(exchange.body)
            embedded = object_value(page.get("_embedded")) if page.get("_embedded") is not None else {}
            conversations = array_value(embedded.get("conversations"))
            if len(conversations) > 100:
                raise ValueError("page holds more conversations than Help Scout's page size")
            page_info = object_value(page.get("page")) if page.get("page") is not None else {}
            links = object_value(page.get("_links")) if page.get("_links") is not None else {}
            page_number = input.page or 1
            output = SearchConversationsOutput(
                tuple(conversation(item) for item in conversations),
                page_number,
                page_number + 1 if links.get("next") is not None and page_number < 10000 else 0,
                int_value(page_info.get("totalPages")),
                int_value(page_info.get("totalElements")),
            )
            return QueryAttempt.branch_result("searched", output, self.client.receipt(call))
        except (ValueError, TypeError, UnicodeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self.client.receipt(call),
                _failure(
                    "searchConversations",
                    FailureKind.PROTOCOL,
                    "Help Scout returned an invalid conversation page: " + str(error),
                ),
            )


class GetConversationOperation:
    """Read a conversation and its newest threads."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated read definition."""
        return definitions.GET_CONVERSATION_DEFINITION

    async def invoke(self, call: Call, input: GetConversationInput) -> QueryAttempt[ConversationDetails]:
        """Read and validate the conversation and thread page."""
        empty = _zero_details()
        if (
            type(input.conversation_id) is not int
            or input.conversation_id < 1
            or type(input.thread_limit) is not int
            or not 0 <= input.thread_limit <= 25
        ):
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                _failure(
                    "getConversation", FailureKind.VALIDATION, "conversationId or threadLimit is invalid"
                ),
            )
        token, resolve_error = await self.client.resolve_token(call)
        if resolve_error:
            return _credential_query("getConversation", resolve_error, empty)
        path = f"/v2/conversations/{input.conversation_id}"
        exchange = await self.client.exchange(call, token, "GET", path)
        if not exchange.error and exchange.status == 301:
            return _merged(self.client, call, input.conversation_id, exchange)
        terminal = _query_exchange(
            self.client,
            call,
            "getConversation",
            exchange,
            empty,
            input.conversation_id,
            "providerRejected",
            "notFound",
        )
        if terminal:
            return terminal
        try:
            current = conversation(parse_json(exchange.body), input.conversation_id)
        except (ValueError, TypeError, UnicodeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self.client.receipt(call, input.conversation_id),
                _failure(
                    "getConversation",
                    FailureKind.PROTOCOL,
                    "Help Scout returned an invalid conversation: " + str(error),
                ),
            )
        threads_exchange = await self.client.exchange(call, token, "GET", path + "/threads", {"page": "1"})
        terminal = _query_exchange(
            self.client,
            call,
            "getConversation",
            threads_exchange,
            empty,
            input.conversation_id,
            "providerRejected",
            "notFound",
        )
        if terminal:
            return terminal
        try:
            page = parse_json(threads_exchange.body)
            embedded = object_value(page.get("_embedded")) if page.get("_embedded") is not None else {}
            all_threads = array_value(embedded.get("threads"))
            if len(all_threads) > 100:
                raise ValueError("page holds more threads than a conversation can")
            limit = input.thread_limit or 10
            links = object_value(page.get("_links")) if page.get("_links") is not None else {}
            output = ConversationDetails(
                current,
                tuple(thread(item) for item in all_threads[:limit]),
                len(all_threads) > limit or links.get("next") is not None,
            )
            return QueryAttempt.branch_result(
                "found", output, self.client.receipt(call, input.conversation_id)
            )
        except (ValueError, TypeError, UnicodeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self.client.receipt(call, input.conversation_id),
                _failure(
                    "getConversation",
                    FailureKind.PROTOCOL,
                    "Help Scout returned an invalid thread page: " + str(error),
                ),
            )


def _merged(
    client: Client, call: Call, conversation_id: int, exchange: Exchange
) -> QueryAttempt[ConversationDetails]:
    location = exchange.headers.get("Location", "")
    parsed = urlsplit(location)
    identifier = parsed.path.removeprefix("/v2/conversations/")
    if (
        (parsed.netloc and (parsed.scheme != "https" or parsed.netloc != "api.helpscout.net"))
        or not identifier.isdecimal()
        or not parsed.path.startswith("/v2/conversations/")
        or int(identifier) < 1
        or int(identifier) == conversation_id
    ):
        return QueryAttempt.branch_result(
            "providerRejected",
            _zero_details(),
            client.receipt(call, conversation_id),
            _failure(
                "getConversation",
                FailureKind.PROTOCOL,
                "Help Scout answered 301 without a conversation Location",
            ),
        )
    target = int(identifier)
    return QueryAttempt.branch_result(
        "merged",
        ConversationDetails(_zero_conversation(), (), False, target),
        client.receipt(call, target),
        _failure(
            "getConversation",
            FailureKind.NOT_FOUND,
            f"conversation {conversation_id} was merged into conversation {target}",
        ),
    )


class FindCustomerByEmailOperation:
    """Find customer profiles by exact email address."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated customer lookup definition."""
        return definitions.FIND_CUSTOMER_BY_EMAIL_DEFINITION

    async def invoke(self, call: Call, input: FindCustomerByEmailInput) -> QueryAttempt[CustomerMatches]:
        """Read and bound the first customer page."""
        output = CustomerMatches(input.email)
        if not _bare_email(input.email):
            return QueryAttempt.branch_result(
                "defect",
                output,
                Receipt(),
                _failure(
                    "findCustomerByEmail",
                    FailureKind.VALIDATION,
                    "email must be one bare email address such as jane@example.com",
                ),
            )
        token, resolve_error = await self.client.resolve_token(call)
        if resolve_error:
            return _credential_query("findCustomerByEmail", resolve_error, CustomerMatches())
        exchange = await self.client.exchange(call, token, "GET", "/v3/customers", {"email": input.email})
        terminal = _query_exchange(
            self.client, call, "findCustomerByEmail", exchange, CustomerMatches(), 0, "providerRejected"
        )
        if terminal:
            return terminal
        try:
            page = parse_json(exchange.body)
            embedded = object_value(page.get("_embedded")) if page.get("_embedded") is not None else {}
            customers = array_value(embedded.get("customers"))
            links = object_value(page.get("_links")) if page.get("_links") is not None else {}
            output = CustomerMatches(
                input.email,
                tuple(customer(item) for item in customers[:50]),
                len(customers) > 50 or links.get("next") is not None,
            )
            if not output.customers:
                return QueryAttempt.branch_result(
                    "notFound",
                    output,
                    self.client.receipt(call),
                    _failure(
                        "findCustomerByEmail",
                        FailureKind.NOT_FOUND,
                        "no Help Scout customer has this email address",
                    ),
                )
            return QueryAttempt.branch_result(
                "found", output, self.client.receipt(call, output.customers[0].id)
            )
        except (ValueError, TypeError, UnicodeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                CustomerMatches(input.email),
                self.client.receipt(call),
                _failure(
                    "findCustomerByEmail",
                    FailureKind.PROTOCOL,
                    "Help Scout returned an invalid customer page: " + str(error),
                ),
            )


class UpdateConversationOperation:
    """Apply absolute status, assignee and tag values to a conversation."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated update definition."""
        return definitions.UPDATE_CONVERSATION_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateConversationInput) -> IdempotencyKey:
        """Correlate repeatable writes with the Dex Call ID."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: UpdateConversationInput
    ) -> MutationAttempt[UpdateConversationOutput]:
        """Read, write only changed values, then read back the final state."""
        empty = _zero_update()
        try:
            _validate_update(input)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), _failure("updateConversation", FailureKind.VALIDATION, str(error))
            )
        token, resolve_error = await self.client.resolve_token(call)
        if resolve_error:
            return _credential_mutation("updateConversation", resolve_error, empty)
        path = f"/v2/conversations/{input.conversation_id}"
        current, terminal = await self._read(call, token, path, input.conversation_id, empty)
        if terminal:
            return terminal
        assert current is not None
        writes = _plan_update(input, current)
        if not writes:
            return MutationAttempt.branch_result(
                "updated",
                UpdateConversationOutput(current, True),
                self.client.receipt(call, input.conversation_id),
            )
        current_output = UpdateConversationOutput(current)
        for method, write_path, payload in writes:
            exchange = await self.client.exchange(call, token, method, write_path, payload=payload)
            terminal = _mutation_exchange(
                self.client,
                call,
                "updateConversation",
                exchange,
                current_output,
                input.conversation_id,
                "providerRejected",
                "notFound",
            )
            if terminal:
                return terminal
        updated, terminal = await self._read(call, token, path, input.conversation_id, current_output)
        if terminal:
            return terminal
        assert updated is not None
        return MutationAttempt.branch_result(
            "updated", UpdateConversationOutput(updated), self.client.receipt(call, input.conversation_id)
        )

    async def _read(
        self,
        call: Call,
        token: str,
        path: str,
        conversation_id: int,
        value: UpdateConversationOutput,
    ) -> tuple[Conversation | None, MutationAttempt[UpdateConversationOutput] | None]:
        exchange = await self.client.exchange(call, token, "GET", path)
        if not exchange.error and exchange.status == 301:
            return None, MutationAttempt.branch_result(
                "notFound",
                value,
                self.client.receipt(call, conversation_id),
                _failure(
                    "updateConversation",
                    FailureKind.NOT_FOUND,
                    "the conversation was merged into another; update that conversation",
                ),
            )
        terminal = _mutation_exchange(
            self.client,
            call,
            "updateConversation",
            exchange,
            value,
            conversation_id,
            "providerRejected",
            "notFound",
        )
        if terminal:
            return None, terminal
        try:
            return conversation(parse_json(exchange.body), conversation_id), None
        except (ValueError, TypeError, UnicodeError) as error:
            return None, MutationAttempt.branch_result(
                "invalidResponse",
                value,
                self.client.receipt(call, conversation_id),
                _failure(
                    "updateConversation",
                    FailureKind.PROTOCOL,
                    "Help Scout returned an invalid conversation: " + str(error),
                ),
            )


def _validate_update(input: UpdateConversationInput) -> None:
    if type(input.conversation_id) is not int or input.conversation_id < 1:
        raise ValueError("conversationId must be a positive Help Scout conversation ID")
    if input.status and not _status(input.status):
        raise ValueError("status must be one of Help Scout's statuses active, pending, closed, or spam")
    if type(input.assignee_id) is not int or input.assignee_id < 0:
        raise ValueError(
            "assigneeId must be a positive Help Scout user or team ID, or zero to keep the assignee"
        )
    if input.is_unassigned and input.assignee_id:
        raise ValueError("isUnassigned cannot be combined with assigneeId")
    _tags("addTags", input.add_tags)
    _tags("removeTags", input.remove_tags)
    if set(tag.lower() for tag in input.add_tags) & set(tag.lower() for tag in input.remove_tags):
        raise ValueError("tag is in both addTags and removeTags")
    if not (input.status or input.assignee_id or input.is_unassigned or input.add_tags or input.remove_tags):
        raise ValueError(
            "at least one of status, assigneeId, isUnassigned, addTags, or removeTags is required"
        )


def _plan_update(input: UpdateConversationInput, current: Conversation) -> list[tuple[str, str, object]]:
    path = f"/v2/conversations/{input.conversation_id}"
    writes: list[tuple[str, str, object]] = []
    if input.status and input.status != current.status:
        writes.append(("PATCH", path, {"op": "replace", "path": "/status", "value": input.status}))
    if input.is_unassigned and current.assignee is not None:
        writes.append(("PATCH", path, {"op": "remove", "path": "/assignTo"}))
    elif input.assignee_id and (current.assignee is None or current.assignee.id != input.assignee_id):
        writes.append(("PATCH", path, {"op": "replace", "path": "/assignTo", "value": input.assignee_id}))
    tags = [tag for tag in current.tags if tag.lower() not in {item.lower() for item in input.remove_tags}]
    changed = len(tags) != len(current.tags)
    for tag in input.add_tags:
        if tag.lower() not in {item.lower() for item in tags}:
            tags.append(tag)
            changed = True
    if changed:
        writes.append(("PUT", path + "/tags", {"tags": tags}))
    return writes


class ReplyToConversationOperation:
    """Fence one nonrepeatable reply or note and reconcile its thread ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated reply definition."""
        return definitions.REPLY_TO_CONVERSATION_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ReplyToConversationInput) -> IdempotencyKey:
        """Record the Dex Call ID without sending a provider idempotency header."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: ReplyToConversationInput) -> MutationAttempt[ConversationReply]:
        """Record a dispatch checkpoint before POST and prohibit ambiguous resends."""
        output = ConversationReply(input.conversation_id, 0, input.is_internal_note, input.customer_id)
        try:
            _validate_reply(input)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                output,
                Receipt(),
                _failure("replyToConversation", FailureKind.VALIDATION, str(error)),
            )
        try:
            fence = Fence(call, timedelta(seconds=30))
            decision = await fence.decide()
        except (ValueError, RuntimeError) as error:
            return MutationAttempt.branch_result(
                "defect",
                output,
                Receipt(),
                _failure("replyToConversation", FailureKind.LOCAL_DEFECT, str(error)),
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return self._uncertain(
                call, output, "the dispatch checkpoint is unavailable, so the thread is not sent again"
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return await self._read_back(call, output, decision.marker)
        token, resolve_error = await self.client.resolve_token(call)
        if resolve_error:
            if resolve_error == "retry":
                try:
                    await fence.record_not_sent()
                except (CheckpointError, DispatchNotAllowedError):
                    pass  # A failed checkpoint cannot authorize another send.
                return MutationAttempt.retry(
                    _failure(
                        "replyToConversation",
                        FailureKind.AVAILABILITY,
                        "the Help Scout access token could not be loaded or obtained yet",
                    )
                )
            return _credential_mutation("replyToConversation", resolve_error, output)
        if not input.is_internal_note and input.customer_id == 0:
            result = await self._read_primary_customer(call, token, output, fence)
            if isinstance(result, MutationAttempt):
                return result
            output = replace(output, customer_id=result)
        path = f"/v2/conversations/{input.conversation_id}/" + (
            "notes" if input.is_internal_note else "reply"
        )
        payload: dict[str, Any] = {"text": input.text}
        if input.status:
            payload["status"] = input.status
        if not input.is_internal_note:
            payload["customer"] = {"id": output.customer_id}
        try:
            exchange = await fence.dispatch(
                lambda _: self.client.exchange(
                    call, token, "POST", path, payload=payload, allow_refresh=False
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                _failure(
                    "replyToConversation",
                    FailureKind.AVAILABILITY,
                    "the thread was not sent before the Execute dispatch deadline",
                )
            )
        except (TimeoutError, DispatchNotAllowedError):
            return self._uncertain(call, output, "the thread may exist because Help Scout's answer was lost")
        if exchange.error == "notBuilt":
            return MutationAttempt.branch_result(
                "defect",
                output,
                Receipt(),
                _failure(
                    "replyToConversation", FailureKind.LOCAL_DEFECT, "Help Scout request could not be built"
                ),
            )
        if exchange.error == "beforeSend":
            try:
                await fence.record_can_resend()
            except (CheckpointError, DispatchNotAllowedError):
                pass  # A failed checkpoint cannot authorize another send.
            return MutationAttempt.retry(
                _failure(
                    "replyToConversation",
                    FailureKind.TRANSPORT,
                    "Help Scout could not be reached, so no thread was sent",
                )
            )
        if exchange.error and not (200 <= exchange.status < 300):
            return self._uncertain(call, output, "the thread may exist because Help Scout's answer was lost")
        if 200 <= exchange.status < 300:
            raw_id = exchange.headers.get("Resource-Id", "").strip()
            thread_id = int(raw_id) if raw_id.isdecimal() else 0
            if thread_id < 1:
                return self._uncertain(
                    call,
                    output,
                    "Help Scout accepted the thread without a valid Resource-Id; the thread may exist",
                )
            output = replace(output, thread_id=thread_id)
            try:
                await fence.record_provider_id(str(thread_id))
            except (CheckpointError, DispatchNotAllowedError):
                pass  # An unrecorded ID still forbids a second send.
            return MutationAttempt.branch_result("replied", output, self.client.receipt(call, thread_id))
        retry, failure, delay, request_id = self.client.classify_failure("replyToConversation", exchange)
        receipt = self.client.receipt(call, output.conversation_id, request_id)
        if exchange.status in (429, 503, 504):
            try:
                await fence.record_can_resend()
            except (CheckpointError, DispatchNotAllowedError):
                pass  # A failed checkpoint cannot authorize another send.
            return MutationAttempt.retry(failure, delay)
        if exchange.status in (404, 410):
            return MutationAttempt.branch_result("notFound", output, receipt, failure)
        if 400 <= exchange.status < 500 and exchange.status not in (408, 409):
            return MutationAttempt.branch_result("providerRejected", output, receipt, failure)
        return MutationAttempt.uncertain(
            output, replace(failure, message=failure.message + "; the thread may exist"), receipt
        )

    async def _read_primary_customer(
        self, call: Call, token: str, output: ConversationReply, fence: Fence
    ) -> int | MutationAttempt[ConversationReply]:
        exchange = await self.client.exchange(
            call, token, "GET", f"/v2/conversations/{output.conversation_id}"
        )
        if not exchange.error and exchange.status == 301:
            return MutationAttempt.branch_result(
                "notFound",
                output,
                self.client.receipt(call, output.conversation_id),
                _failure(
                    "replyToConversation",
                    FailureKind.NOT_FOUND,
                    "the conversation was merged into another; reply to that conversation",
                ),
            )
        terminal = _mutation_exchange(
            self.client,
            call,
            "replyToConversation",
            exchange,
            output,
            output.conversation_id,
            "providerRejected",
            "notFound",
        )
        if terminal:
            if terminal.kind == "retry":
                try:
                    await fence.record_not_sent()
                except (CheckpointError, DispatchNotAllowedError):
                    pass  # A failed checkpoint cannot authorize another send.
            if terminal.branch == "invalidResponse":
                return MutationAttempt.branch_result(
                    "providerRejected", output, terminal.receipt or Receipt(), terminal.failure
                )
            return terminal
        try:
            current = conversation(parse_json(exchange.body), output.conversation_id)
            if current.primary_customer is None:
                return MutationAttempt.branch_result(
                    "providerRejected",
                    output,
                    self.client.receipt(call, output.conversation_id),
                    _failure(
                        "replyToConversation",
                        FailureKind.VALIDATION,
                        "the conversation has no primary customer to reply to; pass customerId",
                    ),
                )
            return current.primary_customer.id
        except (ValueError, TypeError, UnicodeError) as error:
            return MutationAttempt.branch_result(
                "providerRejected",
                output,
                self.client.receipt(call, output.conversation_id),
                _failure(
                    "replyToConversation",
                    FailureKind.PROTOCOL,
                    "Help Scout returned an invalid conversation, so no reply was sent: " + str(error),
                ),
            )

    def _uncertain(
        self, call: Call, output: ConversationReply, message: str
    ) -> MutationAttempt[ConversationReply]:
        return MutationAttempt.uncertain(
            output,
            _failure("replyToConversation", FailureKind.TRANSPORT, message),
            self.client.receipt(call, output.conversation_id),
        )

    async def _read_back(
        self, call: Call, output: ConversationReply, marker: Any
    ) -> MutationAttempt[ConversationReply]:
        thread_id = int(marker.provider_id) if marker is not None and marker.provider_id.isdecimal() else 0
        if marker is None or marker.call_id != call.id or thread_id < 1:
            return self._uncertain(
                call, output, "an earlier attempt may have sent the thread without a confirmable ID"
            )
        token, resolve_error = await self.client.resolve_token(call)
        if resolve_error == "retry":
            return MutationAttempt.retry(
                _failure(
                    "replyToConversation",
                    FailureKind.AVAILABILITY,
                    "the Help Scout access token could not be loaded or obtained yet",
                )
            )
        if resolve_error:
            return self._uncertain(
                call, output, "the accepted thread could not be confirmed because authorization failed"
            )
        path = f"/v2/conversations/{output.conversation_id}"
        for page_number in range(1, 11):
            exchange = await self.client.exchange(
                call, token, "GET", path + "/threads", {"page": str(page_number)}
            )
            if exchange.error in ("tooLarge", "malformed", "notBuilt"):
                return self._uncertain(call, output, "Help Scout returned an unusable thread read-back")
            if exchange.error or exchange.status in (408, 429) or exchange.status >= 500:
                return MutationAttempt.retry(
                    _failure(
                        "replyToConversation",
                        FailureKind.AVAILABILITY,
                        "Help Scout thread read-back is temporarily unavailable",
                    )
                )
            if exchange.status != 200:
                return self._uncertain(call, output, "Help Scout did not confirm the accepted thread")
            try:
                body = parse_json(exchange.body)
                embedded = object_value(body.get("_embedded")) if body.get("_embedded") is not None else {}
                threads = array_value(embedded.get("threads"))
                if len(threads) > 100:
                    raise ValueError("thread page is too large")
                for raw in threads:
                    wire = object_value(raw)
                    if int_value(wire.get("id")) == thread_id:
                        if not output.is_internal_note and not output.customer_id:
                            customer_wire = wire.get("customer")
                            if customer_wire is not None:
                                output = replace(
                                    output, customer_id=int_value(object_value(customer_wire).get("id"))
                                )
                            if not output.customer_id:
                                primary = await self.client.exchange(call, token, "GET", path)
                                if not primary.error and primary.status == 200:
                                    try:
                                        current = conversation(
                                            parse_json(primary.body), output.conversation_id
                                        )
                                        if current.primary_customer is not None:
                                            output = replace(output, customer_id=current.primary_customer.id)
                                    except (ValueError, TypeError, UnicodeError):
                                        pass  # The accepted thread remains confirmed without a customer ID.
                        return MutationAttempt.branch_result(
                            "replied",
                            replace(output, thread_id=thread_id),
                            self.client.receipt(call, thread_id),
                        )
                links = object_value(body.get("_links")) if body.get("_links") is not None else {}
                if links.get("next") is None:
                    break
            except (ValueError, TypeError, UnicodeError):
                return self._uncertain(call, output, "Help Scout returned an unusable thread read-back")
        return self._uncertain(
            call, output, "Help Scout did not confirm the accepted thread in its thread pages"
        )


def _validate_reply(input: ReplyToConversationInput) -> None:
    if type(input.conversation_id) is not int or input.conversation_id < 1:
        raise ValueError("conversationId must be a positive Help Scout conversation ID")
    if not input.text.strip():
        raise ValueError("text is required")
    if len(input.text.encode()) > 65_536:
        raise ValueError("text is longer than 65536 bytes")
    if type(input.customer_id) is not int or input.customer_id < 0:
        raise ValueError(
            "customerId must be a positive Help Scout customer ID, or zero for the primary customer"
        )
    if input.is_internal_note and input.customer_id:
        raise ValueError("customerId applies only to a customer reply, not to an internal note")
    if input.status and not _status(input.status):
        raise ValueError("status must be one of Help Scout's statuses active, pending, closed, or spam")
