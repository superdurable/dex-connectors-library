# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Re:amaze Query and Mutation invocation and branch classification."""

from __future__ import annotations

import re
from datetime import timedelta
from typing import Any

from dex_connectors.sdk import (
    Call,
    CallID,
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

from ._generated import definitions as definitions
from .client import (
    Client,
    CredentialsUnavailableError,
    CredentialsUnusableError,
    Exchange,
    conversation_path,
    is_bare_email_address,
    reamaze_failure,
)
from .decoding import (
    decode_conversation,
    decode_conversation_body,
    decode_conversation_page,
    decode_matching_contacts,
    decode_message,
    decode_message_page,
    parse_document,
)
from .models import (
    Conversation,
    ConversationDetails,
    ConversationMessage,
    ConversationReply,
    CreateConversationInput,
    CreateConversationOutput,
    FindContactByEmailInput,
    FindContactByEmailOutput,
    GetConversationInput,
    ReplyToConversationInput,
    SearchConversationsInput,
    SearchConversationsOutput,
    UpdateConversationInput,
    UpdateConversationOutput,
)

_CONVERSATION_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,254}$")
_CHANNEL = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$")
_TAG = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9 _./+:#&-]{0,62}[A-Za-z0-9_./+:#&-])?$")


def _zero_conversation() -> Conversation:
    return Conversation(status=0)


def _zero_message() -> ConversationMessage:
    return ConversationMessage(visibility=0)


def _zero_output(operation: str, conversation_id: str = "") -> Any:
    return {
        "searchConversations": SearchConversationsOutput(),
        "getConversation": ConversationDetails(_zero_conversation()),
        "createConversation": CreateConversationOutput(_zero_conversation()),
        "updateConversation": UpdateConversationOutput(_zero_conversation()),
        "replyToConversation": ConversationReply(conversation_id, message=_zero_message()),
        "findContactByEmail": FindContactByEmailOutput(),
    }[operation]


def _empty_receipt() -> Receipt:
    return Receipt()


def _query_exchange(
    client: Client, call: Call, operation: str, exchange: Exchange, *, object_id: str = ""
) -> QueryAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    assert exchange.failure is not None
    if exchange.is_retryable_read:
        return QueryAttempt.retry(exchange.failure, exchange.retry_after_seconds)
    branch = {
        "not_found": definitions.NOT_FOUND
        if operation == "getConversation"
        else definitions.PROVIDER_REJECTED,
        "invalid": definitions.INVALID_RESPONSE,
        "defect": definitions.DEFECT,
    }.get(exchange.outcome, definitions.PROVIDER_REJECTED)
    return QueryAttempt.branch_result(
        branch, _zero_output(operation), client.receipt(call, exchange, object_id), exchange.failure
    )


def _mutation_exchange(
    client: Client, call: Call, operation: str, exchange: Exchange, value: Any, *, object_id: str = ""
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    assert exchange.failure is not None
    if exchange.is_retryable_read:
        return MutationAttempt.retry(exchange.failure, exchange.retry_after_seconds)
    branch = {
        "not_found": definitions.NOT_FOUND,
        "invalid": definitions.INVALID_RESPONSE,
        "defect": definitions.DEFECT,
    }.get(exchange.outcome, definitions.PROVIDER_REJECTED)
    return MutationAttempt.branch_result(
        branch, value, client.receipt(call, exchange, object_id), exchange.failure
    )


def _query_credentials_error(operation: str, error: Exception) -> QueryAttempt[Any]:
    if isinstance(error, CredentialsUnavailableError):
        return QueryAttempt.retry(reamaze_failure(FailureKind.AVAILABILITY, operation, str(error)))
    return QueryAttempt.branch_result(
        definitions.DEFECT,
        _zero_output(operation),
        _empty_receipt(),
        reamaze_failure(FailureKind.AUTHENTICATION, operation, str(error)),
    )


def _mutation_credentials_error(operation: str, error: Exception, value: Any) -> MutationAttempt[Any]:
    if isinstance(error, CredentialsUnavailableError):
        return MutationAttempt.retry(reamaze_failure(FailureKind.AVAILABILITY, operation, str(error)))
    return MutationAttempt.branch_result(
        definitions.DEFECT,
        value,
        _empty_receipt(),
        reamaze_failure(FailureKind.AUTHENTICATION, operation, str(error)),
    )


def _validate_conversation_id(value: str, name: str = "conversationId") -> None:
    if not _CONVERSATION_ID.fullmatch(value):
        raise ValueError(
            f"{name} must be a Re:amaze conversation slug of letters, digits, "
            "hyphens, and underscores, such as knock-knock"
        )


def _validate_tags(name: str, tags: tuple[str, ...]) -> None:
    seen: set[str] = set()
    for tag in tags:
        if len(tag.encode()) > 64 or not _TAG.fullmatch(tag):
            raise ValueError(
                f"{name} entry {tag!r} must be 1 to 64 letters, digits, spaces, and "
                "_ . / + : # & - characters without commas, starting with a letter or digit"
            )
        folded = tag.casefold()
        if folded in seen:
            raise ValueError(f"{name} lists {tag!r} twice")
        seen.add(folded)


def _validate_text(name: str, value: str) -> None:
    if not value.strip():
        raise ValueError(f"{name} is required")
    try:
        is_too_long = len(value.encode()) > 16384
    except UnicodeEncodeError:
        is_too_long = True
    if is_too_long:
        raise ValueError(f"{name} must be UTF-8 text of at most 16384 bytes")


def _validate_status(status: int | None) -> None:
    if status is not None and (type(status) is not int or not 0 <= status <= 9):
        raise ValueError(
            f"status {status} is not a Re:amaze status; use 0 (Open), 1 (Responded), "
            "2 (Done), 3 (Spam), 4 (Archived), 5 (On Hold), 6 (Auto-Done), "
            "7 (AI Agent Assigned), 8 (AI Agent Done), or 9 (Spam identified by AI)"
        )


def _validate_hold_until(value: str, status: int | None) -> None:
    if not value:
        return
    if status != 5:
        raise ValueError("holdUntil requires status 5 (On Hold)")
    from .decoding import parse_time

    try:
        parse_time(value)
    except ValueError as error:
        raise ValueError("holdUntil must be an RFC 3339 instant such as 2026-07-15T09:00:00Z") from error


class SearchConversationsOperation:
    """Read one filtered conversation page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest Query identity and branches."""
        return definitions.SEARCH_CONVERSATIONS_DEFINITION

    async def invoke(
        self, call: Call, input: SearchConversationsInput
    ) -> QueryAttempt[SearchConversationsOutput]:
        """Validate filters, send once, and select the exact Query result."""
        operation = "searchConversations"
        try:
            query = _search_query(input)
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.DEFECT,
                SearchConversationsOutput(),
                _empty_receipt(),
                reamaze_failure(FailureKind.VALIDATION, operation, str(error)),
            )
        try:
            credentials = await self.client.resolve_credentials(call)
        except (CredentialsUnavailableError, CredentialsUnusableError) as error:
            return _query_credentials_error(operation, error)
        exchange = await self.client.exchange(
            call, credentials, operation, "GET", "/conversations", query=query
        )
        classified = _query_exchange(self.client, call, operation, exchange)
        if classified is not None:
            return classified
        try:
            output = decode_conversation_page(exchange.body, input.page or 1)
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                SearchConversationsOutput(),
                self.client.receipt(call, exchange),
                reamaze_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Re:amaze returned an invalid conversation page: " + str(error),
                ),
            )
        return QueryAttempt.branch_result(definitions.SEARCHED, output, self.client.receipt(call, exchange))


def _search_query(input: SearchConversationsInput) -> dict[str, str]:
    if input.filter not in (None, "", "open", "unassigned", "archived", "all"):
        raise ValueError(f"filter {input.filter!r} must be blank, open, unassigned, archived, or all")
    if input.sort not in (None, "", "updated", "changed"):
        raise ValueError(f"sort {input.sort!r} must be blank, updated, or changed")
    if not 0 <= input.page <= 10000:
        raise ValueError("page must be between 1 and 10000, or zero for the first page")
    query = {"page": str(input.page or 1)}
    if input.filter:
        query["filter"] = input.filter
    if input.requester_email:
        if not is_bare_email_address(input.requester_email):
            raise ValueError("requesterEmail must be one bare email address such as jane@example.com")
        query["for"] = input.requester_email
    _validate_tags("tags", input.tags)
    if input.tags:
        query["tag"] = ",".join(input.tags)
    if input.channel:
        if not _CHANNEL.fullmatch(input.channel):
            raise ValueError(
                "channel must be a Re:amaze channel slug of letters, digits, "
                "hyphens, and underscores, such as support"
            )
        query["category"] = input.channel
    if input.sort:
        query["sort"] = input.sort
    from .decoding import parse_time

    for name, value, parameter in (
        ("customerMessageSince", input.customer_message_since, "start_date"),
        ("customerMessageUntil", input.customer_message_until, "end_date"),
    ):
        if value:
            try:
                parse_time(value)
            except ValueError as error:
                raise ValueError(
                    f"{name} must be an RFC 3339 instant such as 2026-01-21T00:00:00Z"
                ) from error
            query[parameter] = value
    return query


class GetConversationOperation:
    """Read a conversation and its latest message page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest Query identity and branches."""
        return definitions.GET_CONVERSATION_DEFINITION

    async def invoke(self, call: Call, input: GetConversationInput) -> QueryAttempt[ConversationDetails]:
        """Read the conversation then its first bounded message page."""
        operation = "getConversation"
        try:
            _validate_conversation_id(input.conversation_id)
            if not 0 <= input.message_limit <= 30:
                raise ValueError("messageLimit must be between 1 and 30, or zero for 10")
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.DEFECT,
                _zero_output(operation),
                _empty_receipt(),
                reamaze_failure(FailureKind.VALIDATION, operation, str(error)),
            )
        try:
            credentials = await self.client.resolve_credentials(call)
        except (CredentialsUnavailableError, CredentialsUnusableError) as error:
            return _query_credentials_error(operation, error)
        path = conversation_path(input.conversation_id)
        first = await self.client.exchange(call, credentials, operation, "GET", path)
        classified = _query_exchange(self.client, call, operation, first, object_id=input.conversation_id)
        if classified is not None:
            return classified
        try:
            conversation = decode_conversation_body(first.body, input.conversation_id)
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                _zero_output(operation),
                self.client.receipt(call, first, input.conversation_id),
                reamaze_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Re:amaze returned an invalid conversation: " + str(error),
                ),
            )
        second = await self.client.exchange(
            call, credentials, operation, "GET", path + "/messages", query={"page": "1"}
        )
        classified = _query_exchange(self.client, call, operation, second, object_id=input.conversation_id)
        if classified is not None:
            return classified
        try:
            messages, page_count, total_count = decode_message_page(second.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                _zero_output(operation),
                self.client.receipt(call, second, input.conversation_id),
                reamaze_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Re:amaze returned an invalid message page: " + str(error),
                ),
            )
        limit = input.message_limit or 10
        output = ConversationDetails(
            conversation,
            messages[:limit],
            len(messages) > limit or page_count > 1 or total_count > min(len(messages), limit),
        )
        return QueryAttempt.branch_result(
            definitions.FOUND, output, self.client.receipt(call, second, input.conversation_id)
        )


class FindContactByEmailOperation:
    """Resolve exact contact emails from Re:amaze's fuzzy candidate search."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest Query identity and branches."""
        return definitions.FIND_CONTACT_BY_EMAIL_DEFINITION

    async def invoke(
        self, call: Call, input: FindContactByEmailInput
    ) -> QueryAttempt[FindContactByEmailOutput]:
        """Read one candidate page and keep only case-insensitive exact email matches."""
        operation = "findContactByEmail"
        if not is_bare_email_address(input.email):
            return QueryAttempt.branch_result(
                definitions.DEFECT,
                FindContactByEmailOutput(),
                _empty_receipt(),
                reamaze_failure(
                    FailureKind.VALIDATION,
                    operation,
                    "email must be one bare email address such as jane@example.com",
                ),
            )
        try:
            credentials = await self.client.resolve_credentials(call)
        except (CredentialsUnavailableError, CredentialsUnusableError) as error:
            return _query_credentials_error(operation, error)
        exchange = await self.client.exchange(
            call,
            credentials,
            operation,
            "GET",
            "/contacts",
            query={"q": input.email, "type": "email", "page": "1"},
        )
        classified = _query_exchange(self.client, call, operation, exchange)
        if classified is not None:
            return classified
        try:
            output = decode_matching_contacts(exchange.body, input.email)
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                FindContactByEmailOutput(),
                self.client.receipt(call, exchange),
                reamaze_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Re:amaze returned an invalid contact page: " + str(error),
                ),
            )
        branch = definitions.FOUND if output.contacts else definitions.NOT_FOUND
        return QueryAttempt.branch_result(branch, output, self.client.receipt(call, exchange))


class UpdateConversationOperation:
    """Set absolute conversation values after reading current provider state."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest Mutation identity and branches."""
        return definitions.UPDATE_CONVERSATION_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateConversationInput) -> IdempotencyKey:
        """Use the stable Call ID to correlate this repeatable absolute write."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: UpdateConversationInput
    ) -> MutationAttempt[UpdateConversationOutput]:
        """Read current state, skip an applied change, or PUT only differing fields."""
        operation = "updateConversation"
        try:
            _validate_conversation_id(input.conversation_id)
            _validate_status(input.status)
            _validate_hold_until(input.hold_until, input.status)
            if input.assignee_email and not is_bare_email_address(input.assignee_email):
                raise ValueError(
                    "assigneeEmail must be one bare staff email address such as agent@example.com"
                )
            _validate_tags("addTags", input.add_tags)
            _validate_tags("removeTags", input.remove_tags)
            if {tag.casefold() for tag in input.add_tags} & {tag.casefold() for tag in input.remove_tags}:
                raise ValueError("a tag is in both addTags and removeTags")
            if (
                input.status is None
                and not input.assignee_email
                and not input.add_tags
                and not input.remove_tags
            ):
                raise ValueError("at least one of status, assigneeEmail, addTags, or removeTags is required")
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                _zero_output(operation),
                _empty_receipt(),
                reamaze_failure(FailureKind.VALIDATION, operation, str(error)),
            )
        try:
            credentials = await self.client.resolve_credentials(call)
        except (CredentialsUnavailableError, CredentialsUnusableError) as error:
            return _mutation_credentials_error(operation, error, _zero_output(operation))
        path = conversation_path(input.conversation_id)
        read = await self.client.exchange(call, credentials, operation, "GET", path)
        classified = _mutation_exchange(
            self.client, call, operation, read, _zero_output(operation), object_id=input.conversation_id
        )
        if classified is not None:
            return classified
        try:
            current = decode_conversation_body(read.body, input.conversation_id)
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                _zero_output(operation),
                self.client.receipt(call, read, input.conversation_id),
                reamaze_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Re:amaze returned an invalid conversation: " + str(error),
                ),
            )
        current_output = UpdateConversationOutput(current)
        change = _plan_conversation_change(input, current)
        if not change:
            return MutationAttempt.branch_result(
                definitions.UPDATED,
                UpdateConversationOutput(current, True),
                self.client.receipt(call, read, input.conversation_id),
            )
        write = await self.client.exchange(
            call, credentials, operation, "PUT", path, payload={"conversation": change}
        )
        classified = _mutation_exchange(
            self.client, call, operation, write, current_output, object_id=input.conversation_id
        )
        if classified is not None:
            return classified
        try:
            updated = decode_conversation_body(write.body, input.conversation_id)
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                current_output,
                self.client.receipt(call, write, input.conversation_id),
                reamaze_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Re:amaze returned an invalid updated conversation: " + str(error),
                ),
            )
        return MutationAttempt.branch_result(
            definitions.UPDATED,
            UpdateConversationOutput(updated),
            self.client.receipt(call, write, input.conversation_id),
        )


def _plan_conversation_change(input: UpdateConversationInput, current: Conversation) -> dict[str, Any]:
    change: dict[str, Any] = {}
    if input.status is not None and (input.status != current.status or input.hold_until):
        change["status"] = input.status
        if input.hold_until:
            change["hold_until"] = input.hold_until
    if input.assignee_email and (
        current.assignee is None or current.assignee.email.casefold() != input.assignee_email.casefold()
    ):
        change["assignee"] = {"email": input.assignee_email}
    removed = {tag.casefold() for tag in input.remove_tags}
    tags = [tag for tag in current.tags if tag.casefold() not in removed]
    for tag in input.add_tags:
        if tag.casefold() not in {candidate.casefold() for candidate in tags}:
            tags.append(tag)
    if tuple(tags) != current.tags:
        change["tag_list"] = tags
    return change


_UNCERTAIN_NOT_FOUND = (
    "an earlier attempt may have sent the request without a confirmed outcome, "
    "and Re:amaze shows no record with its key"
)


async def _fenced_credential_error(
    fence: Fence, operation: str, error: Exception, value: Any
) -> MutationAttempt[Any]:
    try:
        await fence.record_not_sent()
    except Exception:
        return MutationAttempt.retry(reamaze_failure(FailureKind.AVAILABILITY, operation, str(error)))
    if isinstance(error, CredentialsUnusableError):
        return MutationAttempt.branch_result(
            definitions.DEFECT,
            value,
            _empty_receipt(),
            reamaze_failure(
                FailureKind.AUTHENTICATION,
                operation,
                "Re:amaze connection credentials are unusable; reconnect the connection in Dex Web",
            ),
        )
    return _mutation_credentials_error(operation, error, value)


async def _fenced_exchange_result(
    fence: Fence,
    client: Client,
    call: Call,
    operation: str,
    exchange: Exchange,
    value: Any,
    *,
    object_id: str = "",
    has_not_found: bool = False,
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    assert exchange.failure is not None
    receipt = client.receipt(call, exchange, object_id)
    if exchange.outcome in {"rate_limited", "not_sent"} or exchange.status == 503:
        try:
            await fence.record_can_resend()
        except Exception:
            return MutationAttempt.retry(
                reamaze_failure(
                    FailureKind.AVAILABILITY, operation, "retry checkpoint could not be recorded"
                ),
                exchange.retry_after_seconds,
            )
    if exchange.is_retryable_read:
        return MutationAttempt.retry(exchange.failure, exchange.retry_after_seconds)
    if exchange.outcome == "invalid":
        return MutationAttempt.uncertain(value, exchange.failure, receipt)
    if exchange.outcome == "not_found":
        branch = definitions.NOT_FOUND if has_not_found else definitions.PROVIDER_REJECTED
    elif exchange.outcome == "defect":
        branch = definitions.DEFECT
    elif exchange.status == 409:
        return MutationAttempt.retry(exchange.failure)
    else:
        branch = definitions.PROVIDER_REJECTED
    return MutationAttempt.branch_result(branch, value, receipt, exchange.failure)


def _dispatch_key(call: Call) -> str:
    return "dex-" + str(call.idempotency_key or call.id)


def _validate_create(input: CreateConversationInput) -> None:
    _validate_text("subject", input.subject)
    _validate_text("message", input.message)
    if not _CHANNEL.fullmatch(input.channel):
        raise ValueError(
            "channel must be a Re:amaze channel slug of letters, digits, "
            "hyphens, and underscores, such as support"
        )
    if not is_bare_email_address(input.requester.email):
        raise ValueError("requester.email must be one bare email address such as jane@example.com")
    if input.requester.name != input.requester.name.strip() or len(input.requester.name.encode()) > 255:
        raise ValueError("requester.name must be at most 255 bytes without surrounding spaces")
    _validate_status(input.status)
    _validate_hold_until(input.hold_until, input.status)
    if input.assignee_email and not is_bare_email_address(input.assignee_email):
        raise ValueError("assigneeEmail must be one bare staff email address such as agent@example.com")
    _validate_tags("tags", input.tags)


def _create_payload(input: CreateConversationInput, key: str) -> dict[str, object]:
    user: dict[str, object] = {"email": input.requester.email}
    message: dict[str, object] = {"body": input.message}
    conversation: dict[str, object] = {
        "subject": input.subject,
        "category": input.channel,
        "data": {"dex_dispatch_key": key},
        "message": message,
        "user": user,
    }
    if input.requester.name:
        user["name"] = input.requester.name
    if input.tags:
        conversation["tag_list"] = list(input.tags)
    if input.status is not None:
        conversation["status"] = input.status
    if input.hold_until:
        conversation["hold_until"] = input.hold_until
    if input.assignee_email:
        conversation["assignee"] = input.assignee_email
    if input.should_suppress_notifications:
        message["suppress_notifications"] = True
    return {"conversation": conversation}


class CreateConversationOperation:
    """Fence a conversation POST and reconcile its durable dispatch key."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest Mutation identity and branches."""
        return definitions.CREATE_CONVERSATION_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateConversationInput) -> IdempotencyKey:
        """Use the Call ID as a Re:amaze-visible conversation data key."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: CreateConversationInput
    ) -> MutationAttempt[CreateConversationOutput]:
        """Send once under a Dex heartbeat fence and read back uncertain writes."""
        operation = "createConversation"
        zero = _zero_output(operation)
        try:
            _validate_create(input)
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                zero,
                _empty_receipt(),
                reamaze_failure(FailureKind.VALIDATION, operation, str(error)),
            )
        try:
            fence = Fence(call, timedelta(seconds=30))
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                zero,
                _empty_receipt(),
                reamaze_failure(FailureKind.LOCAL_DEFECT, operation, str(error)),
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.uncertain(
                zero,
                reamaze_failure(FailureKind.TRANSPORT, operation, "dispatch checkpoint cannot be trusted"),
                self.client.receipt(call, Exchange("")),
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            try:
                credentials = await self.client.resolve_credentials(call)
            except CredentialsUnavailableError:
                return MutationAttempt.retry(
                    reamaze_failure(
                        FailureKind.AVAILABILITY,
                        operation,
                        "connection credentials could not be loaded to read back an earlier write",
                    )
                )
            except CredentialsUnusableError:
                return MutationAttempt.uncertain(
                    zero,
                    reamaze_failure(
                        FailureKind.AUTHENTICATION,
                        operation,
                        "the earlier write cannot be read back; reconnect the connection in Dex Web",
                    ),
                    self.client.receipt(
                        call, Exchange(""), decision.marker.provider_id if decision.marker else ""
                    ),
                )
            if decision.marker and decision.marker.provider_id:
                return await self._confirm_candidate(call, credentials, decision.marker.provider_id)
            return await self._reconcile(call, credentials)
        try:
            credentials = await self.client.resolve_credentials(call)
        except (CredentialsUnavailableError, CredentialsUnusableError) as error:
            return await _fenced_credential_error(fence, operation, error, zero)
        try:
            exchange = await fence.dispatch(
                lambda _: self.client.exchange(
                    call,
                    credentials,
                    operation,
                    "POST",
                    "/conversations",
                    payload=_create_payload(input, _dispatch_key(call)),
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                reamaze_failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    "write was not sent before the attempt deadline or checkpoint",
                )
            )
        except (DispatchNotAllowedError, TimeoutError):
            return MutationAttempt.uncertain(
                zero,
                reamaze_failure(
                    FailureKind.TRANSPORT, operation, "write dispatch outcome could not be confirmed"
                ),
                _empty_receipt(),
            )
        if 200 <= exchange.status < 300 and exchange.outcome != "succeeded":
            return await self._reconcile(call, credentials)
        classified = await _fenced_exchange_result(fence, self.client, call, operation, exchange, zero)
        if classified is not None:
            return classified
        try:
            conversation = decode_conversation_body(exchange.body)
        except ValueError:
            return await self._reconcile(call, credentials)
        try:
            await fence.record_provider_id(conversation.id)
        except Exception:
            pass  # The accepted response already identifies the write; later attempts can search its key.
        return MutationAttempt.branch_result(
            definitions.CREATED,
            CreateConversationOutput(conversation),
            self.client.receipt(call, exchange, conversation.id),
        )

    async def _reconcile(self, call: Call, credentials: Any) -> MutationAttempt[CreateConversationOutput]:
        operation, key = "createConversation", _dispatch_key(call)
        zero = _zero_output(operation)
        exchange = await self.client.exchange(
            call,
            credentials,
            operation,
            "GET",
            "/conversations",
            query={"filter": "all", "data[dex_dispatch_key]": key},
        )
        classified = _reconciliation_result(self.client, call, operation, exchange, zero)
        if classified is not None:
            return classified
        try:
            document = parse_document(exchange.body)
            from .decoding import validate_page

            items, _, _ = validate_page(document, "conversations")
            candidate = ""
            for item in items:
                if not isinstance(item, dict):
                    continue
                data = item.get("data")
                if data is None and not candidate and _CONVERSATION_ID.fullmatch(item.get("slug") or ""):
                    candidate = item["slug"]
                if isinstance(data, dict) and data.get("dex_dispatch_key") == key:
                    conversation = decode_conversation(item)
                    return MutationAttempt.branch_result(
                        definitions.CREATED,
                        CreateConversationOutput(conversation, True),
                        self.client.receipt(call, exchange, conversation.id),
                    )
        except ValueError as error:
            return MutationAttempt.uncertain(
                zero,
                reamaze_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    (
                        "an earlier attempt of this Step sent the request, and the "
                        "conversation list read back is invalid: "
                    )
                    + str(error),
                ),
                self.client.receipt(call, exchange),
            )
        if candidate:
            return await self._confirm_candidate(call, credentials, candidate)
        return MutationAttempt.uncertain(
            zero,
            reamaze_failure(FailureKind.TRANSPORT, operation, _UNCERTAIN_NOT_FOUND),
            self.client.receipt(call, exchange),
        )

    async def _confirm_candidate(
        self, call: Call, credentials: Any, slug: str
    ) -> MutationAttempt[CreateConversationOutput]:
        operation, zero = "createConversation", _zero_output("createConversation")
        exchange = await self.client.exchange(call, credentials, operation, "GET", conversation_path(slug))
        classified = _reconciliation_result(self.client, call, operation, exchange, zero, slug)
        if classified is not None:
            return classified
        try:
            document = parse_document(exchange.body)
            if not isinstance(document, dict) or (document.get("data") or {}).get(
                "dex_dispatch_key"
            ) != _dispatch_key(call):
                return MutationAttempt.uncertain(
                    zero,
                    reamaze_failure(FailureKind.TRANSPORT, operation, _UNCERTAIN_NOT_FOUND),
                    self.client.receipt(call, exchange, slug),
                )
            conversation = decode_conversation(document, expected_id=slug)
        except ValueError as error:
            return MutationAttempt.uncertain(
                zero,
                reamaze_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    (
                        "an earlier attempt of this Step sent the request, and the "
                        "conversation read back is invalid: "
                    )
                    + str(error),
                ),
                self.client.receipt(call, exchange, slug),
            )
        return MutationAttempt.branch_result(
            definitions.CREATED,
            CreateConversationOutput(conversation, True),
            self.client.receipt(call, exchange, slug),
        )


def _reconciliation_result(
    client: Client, call: Call, operation: str, exchange: Exchange, value: Any, object_id: str = ""
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    assert exchange.failure is not None
    if exchange.is_retryable_read:
        return MutationAttempt.retry(exchange.failure, exchange.retry_after_seconds)
    failure = reamaze_failure(
        exchange.failure.kind,
        operation,
        "an earlier attempt may have sent the request, and it could not be read back: "
        + exchange.failure.message,
    )
    return MutationAttempt.uncertain(value, failure, client.receipt(call, exchange, object_id))


class ReplyToConversationOperation:
    """Fence a reply POST and reconcile its origin ID on uncertain dispatch."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest Mutation identity and branches."""
        return definitions.REPLY_TO_CONVERSATION_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ReplyToConversationInput) -> IdempotencyKey:
        """Use the stable Call ID as the provider message origin ID."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: ReplyToConversationInput) -> MutationAttempt[ConversationReply]:
        """Send one reply or note, then read the message page after ambiguous acceptance."""
        operation = "replyToConversation"
        zero = _zero_output(operation, input.conversation_id)
        try:
            _validate_conversation_id(input.conversation_id)
            _validate_text("text", input.text)
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                zero,
                _empty_receipt(),
                reamaze_failure(FailureKind.VALIDATION, operation, str(error)),
            )
        try:
            fence = Fence(call, timedelta(seconds=30))
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                zero,
                _empty_receipt(),
                reamaze_failure(FailureKind.LOCAL_DEFECT, operation, str(error)),
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.uncertain(
                zero,
                reamaze_failure(FailureKind.TRANSPORT, operation, "dispatch checkpoint cannot be trusted"),
                self.client.receipt(call, Exchange("")),
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            try:
                credentials = await self.client.resolve_credentials(call)
            except CredentialsUnavailableError:
                return MutationAttempt.retry(
                    reamaze_failure(
                        FailureKind.AVAILABILITY,
                        operation,
                        "connection credentials could not be loaded to read back an earlier write",
                    )
                )
            except CredentialsUnusableError:
                return MutationAttempt.uncertain(
                    zero,
                    reamaze_failure(
                        FailureKind.AUTHENTICATION,
                        operation,
                        "the earlier write cannot be read back; reconnect the connection in Dex Web",
                    ),
                    self.client.receipt(call, Exchange(""), input.conversation_id),
                )
            return await self._reconcile(call, credentials, input)
        try:
            credentials = await self.client.resolve_credentials(call)
        except (CredentialsUnavailableError, CredentialsUnusableError) as error:
            return await _fenced_credential_error(fence, operation, error, zero)
        message: dict[str, object] = {
            "body": input.text,
            "visibility": 1 if input.is_internal_note else 0,
            "origin_id": _dispatch_key(call),
        }
        if input.should_suppress_notifications:
            message["suppress_notifications"] = True
        if input.should_suppress_auto_resolve:
            message["suppress_autoresolve"] = True
        try:
            exchange = await fence.dispatch(
                lambda _: self.client.exchange(
                    call,
                    credentials,
                    operation,
                    "POST",
                    conversation_path(input.conversation_id) + "/messages",
                    payload={"message": message},
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                reamaze_failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    "write was not sent before the attempt deadline or checkpoint",
                )
            )
        except (DispatchNotAllowedError, TimeoutError):
            return MutationAttempt.uncertain(
                zero,
                reamaze_failure(
                    FailureKind.TRANSPORT, operation, "write dispatch outcome could not be confirmed"
                ),
                _empty_receipt(),
            )
        if 200 <= exchange.status < 300 and exchange.outcome != "succeeded":
            return await self._reconcile(call, credentials, input)
        classified = await _fenced_exchange_result(
            fence,
            self.client,
            call,
            operation,
            exchange,
            _zero_output(operation),
            object_id=input.conversation_id,
            has_not_found=True,
        )
        if classified is not None:
            return classified
        try:
            document = parse_document(exchange.body)
            if not isinstance(document, dict):
                raise ValueError("response is not a message object")
            conversation = document.get("conversation")
            if isinstance(conversation, dict) and conversation.get("slug") not in (
                None,
                "",
                input.conversation_id,
            ):
                raise ValueError("response is for another conversation")
            decoded = decode_message(document)
        except ValueError:
            return await self._reconcile(call, credentials, input)
        return MutationAttempt.branch_result(
            definitions.REPLIED,
            ConversationReply(input.conversation_id, message=decoded),
            self.client.receipt(call, exchange, input.conversation_id),
        )

    async def _reconcile(
        self, call: Call, credentials: Any, input: ReplyToConversationInput
    ) -> MutationAttempt[ConversationReply]:
        operation, zero = "replyToConversation", _zero_output("replyToConversation", input.conversation_id)
        exchange = await self.client.exchange(
            call,
            credentials,
            operation,
            "GET",
            conversation_path(input.conversation_id) + "/messages",
            query={"page": "1"},
        )
        classified = _reconciliation_result(
            self.client, call, operation, exchange, zero, input.conversation_id
        )
        if classified is not None:
            return classified
        try:
            messages, _, _ = decode_message_page(exchange.body)
        except ValueError as error:
            return MutationAttempt.uncertain(
                zero,
                reamaze_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    (
                        "an earlier attempt of this Step sent the message, and the "
                        "message page read back is invalid: "
                    )
                    + str(error),
                ),
                self.client.receipt(call, exchange, input.conversation_id),
            )
        for message in messages:
            if message.origin_id == _dispatch_key(call):
                return MutationAttempt.branch_result(
                    definitions.REPLIED,
                    ConversationReply(input.conversation_id, message=message, was_already_applied=True),
                    self.client.receipt(call, exchange, input.conversation_id),
                )
        return MutationAttempt.uncertain(
            zero,
            reamaze_failure(FailureKind.TRANSPORT, operation, _UNCERTAIN_NOT_FOUND),
            self.client.receipt(call, exchange, input.conversation_id),
        )
