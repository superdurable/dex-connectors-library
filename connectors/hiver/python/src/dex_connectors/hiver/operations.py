# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Hiver operation invokes with Go-compatible branches and retry decisions."""

import asyncio
import re
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from email._header_value_parser import get_addr_spec
from email.errors import NonASCIILocalPartDefect
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

from ._generated.definitions import (
    ADD_NOTE_DEFINITION,
    CREATE_SHARED_DRAFT_DEFINITION,
    GET_CONVERSATION_DEFINITION,
    LIST_CONVERSATIONS_DEFINITION,
    LIST_INBOXES_DEFINITION,
    UPDATE_CONVERSATION_DEFINITION,
)
from .client import (
    Client,
    Exchange,
    decode_conversation,
    decode_draft,
    decode_inbox,
    decode_note,
    decode_tag,
    hiver_failure,
    list_page,
    page_query,
    path_escape,
    single_data,
    string_field,
    validate_id,
    validate_page,
    wire_id,
)
from .models import (
    AddNoteInput,
    AddNoteOutput,
    Conversation,
    ConversationDetails,
    CreateSharedDraftInput,
    GetConversationInput,
    ListConversationsInput,
    ListConversationsOutput,
    ListInboxesInput,
    ListInboxesOutput,
    SharedDraft,
    Tag,
    UpdateConversationInput,
    UpdateConversationOutput,
)


def _query_result(
    call: Call,
    client: Client,
    exchange: Exchange,
    empty: Any,
    *,
    not_found: str = "notFound",
    object_id: str = "",
) -> QueryAttempt[Any] | None:
    if exchange.outcome == "success":
        return None
    assert exchange.failure is not None
    if exchange.outcome in ("rateLimited", "notSent", "unavailable"):
        return QueryAttempt.retry(exchange.failure, exchange.retry_after)
    route = {"notFound": not_found, "invalid": "invalidResponse", "defect": "defect"}.get(
        exchange.outcome, "providerRejected"
    )
    return QueryAttempt.branch_result(
        route, empty, client.receipt(call, exchange, object_id), exchange.failure
    )


def _mutation_result(
    call: Call, client: Client, exchange: Exchange, empty: Any, object_id: str
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "success":
        return None
    assert exchange.failure is not None
    if exchange.outcome in ("rateLimited", "notSent", "unavailable"):
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    route = {"notFound": "notFound", "invalid": "invalidResponse", "defect": "defect"}.get(
        exchange.outcome, "providerRejected"
    )
    return MutationAttempt.branch_result(
        route, empty, client.receipt(call, exchange, object_id), exchange.failure
    )


def _empty_details() -> ConversationDetails:
    return ConversationDetails(Conversation("", "", status=""), ())


def _empty_update() -> UpdateConversationOutput:
    return UpdateConversationOutput(Conversation("", "", status=""))


def _validation_query(operation: str, empty: Any, message: str) -> QueryAttempt[Any]:
    return QueryAttempt.branch_result(
        "defect", empty, Receipt(), hiver_failure(FailureKind.VALIDATION, operation, message)
    )


def _validation_mutation(operation: str, empty: Any, message: str) -> MutationAttempt[Any]:
    return MutationAttempt.branch_result(
        "defect", empty, Receipt(), hiver_failure(FailureKind.VALIDATION, operation, message)
    )


class ListInboxesOperation:
    """List one page of Hiver shared inboxes."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest's Query identity."""
        return LIST_INBOXES_DEFINITION

    async def invoke(self, call: Call, input: ListInboxesInput) -> QueryAttempt[ListInboxesOutput]:
        """Send one inbox page request and decode its result."""
        empty = ListInboxesOutput()
        try:
            validate_page(input.page_token, input.page_size)
        except ValueError as error:
            return _validation_query("listInboxes", empty, str(error))
        token = await self.client.token(call)
        if token is None:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                hiver_failure(
                    FailureKind.AUTHENTICATION, "listInboxes", "connection credentials are unavailable"
                ),
            )
        exchange = await self.client.exchange(
            call, token, "listInboxes", "GET", "/inboxes", page_query(input.page_token, input.page_size)
        )
        terminal = _query_result(call, self.client, exchange, empty, not_found="providerRejected")
        if terminal:
            return terminal
        try:
            results, next_page = list_page(exchange.body)
            inboxes = tuple(decode_inbox(raw) for raw in results)
        except (ValueError, TypeError, AttributeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self.client.receipt(call, exchange),
                hiver_failure(
                    FailureKind.PROTOCOL, "listInboxes", "Hiver returned an invalid inbox page: " + str(error)
                ),
            )
        return QueryAttempt.branch_result(
            "listed", ListInboxesOutput(inboxes, next_page), self.client.receipt(call, exchange)
        )


class ListConversationsOperation:
    """List one page of conversations for one shared inbox."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest's Query identity."""
        return LIST_CONVERSATIONS_DEFINITION

    async def invoke(
        self, call: Call, input: ListConversationsInput
    ) -> QueryAttempt[ListConversationsOutput]:
        """Send one conversation page request and decode its result."""
        empty = ListConversationsOutput()
        try:
            validate_id("inboxId", input.inbox_id)
            validate_page(input.page_token, input.page_size)
        except ValueError as error:
            return _validation_query("listConversations", empty, str(error))
        token = await self.client.token(call)
        if token is None:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                hiver_failure(
                    FailureKind.AUTHENTICATION, "listConversations", "connection credentials are unavailable"
                ),
            )
        exchange = await self.client.exchange(
            call,
            token,
            "listConversations",
            "GET",
            "/inboxes/" + path_escape(input.inbox_id) + "/conversations",
            page_query(input.page_token, input.page_size),
        )
        terminal = _query_result(call, self.client, exchange, empty, object_id=input.inbox_id)
        if terminal:
            return terminal
        receipt = self.client.receipt(call, exchange, input.inbox_id)
        try:
            results, next_page = list_page(exchange.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                hiver_failure(
                    FailureKind.PROTOCOL,
                    "listConversations",
                    "Hiver returned an invalid conversation page: " + str(error),
                ),
            )
        conversations = []
        for index, raw in enumerate(results):
            try:
                conversations.append(decode_conversation(raw, input.inbox_id).conversation)
            except (ValueError, TypeError, AttributeError) as error:
                return QueryAttempt.branch_result(
                    "invalidResponse",
                    empty,
                    receipt,
                    hiver_failure(
                        FailureKind.PROTOCOL,
                        "listConversations",
                        f"Hiver returned an invalid conversation at index {index}: {error}",
                    ),
                )
        return QueryAttempt.branch_result(
            "listed", ListConversationsOutput(tuple(conversations), next_page), receipt
        )


class GetConversationOperation:
    """Read one Hiver conversation and its message IDs."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest's Query identity."""
        return GET_CONVERSATION_DEFINITION

    async def invoke(self, call: Call, input: GetConversationInput) -> QueryAttempt[ConversationDetails]:
        """Read and decode one conversation."""
        empty = _empty_details()
        try:
            validate_id("inboxId", input.inbox_id)
            validate_id("conversationId", input.conversation_id)
        except ValueError as error:
            return _validation_query("getConversation", empty, str(error))
        token = await self.client.token(call)
        if token is None:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                hiver_failure(
                    FailureKind.AUTHENTICATION, "getConversation", "connection credentials are unavailable"
                ),
            )
        exchange = await self.client.exchange(
            call, token, "getConversation", "GET", _conversation_path(input.inbox_id, input.conversation_id)
        )
        terminal = _query_result(call, self.client, exchange, empty, object_id=input.conversation_id)
        if terminal:
            return terminal
        try:
            details = decode_conversation(single_data(exchange.body), input.inbox_id)
        except (ValueError, TypeError, AttributeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self.client.receipt(call, exchange, input.conversation_id),
                hiver_failure(
                    FailureKind.PROTOCOL,
                    "getConversation",
                    "Hiver returned an invalid conversation: " + str(error),
                ),
            )
        return QueryAttempt.branch_result(
            "found", details, self.client.receipt(call, exchange, details.conversation.id)
        )


def _conversation_path(inbox_id: str, conversation_id: str) -> str:
    return "/inboxes/" + path_escape(inbox_id) + "/conversations/" + path_escape(conversation_id)


_GO_WHITESPACE = (
    "\t\n\v\f\r \u0085\u00a0\u1680"
    "\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a"
    "\u2028\u2029\u202f\u205f\u3000"
)


def _validate_text(name: str, value: str, limit: int) -> None:
    if not value.strip(_GO_WHITESPACE):
        raise ValueError(f"{name} is required")
    if len(value.encode()) > limit:
        raise ValueError(f"{name} exceeds {limit} bytes")
    if "\0" in value:
        raise ValueError(f"{name} must not contain NUL characters")


def _validate_update(input: UpdateConversationInput) -> None:
    validate_id("inboxId", input.inbox_id)
    validate_id("conversationId", input.conversation_id)
    if input.status and input.status not in ("open", "pending", "closed"):
        raise ValueError("status must be open, pending, or closed")
    if input.assignee_email:
        try:
            parsed, remainder = get_addr_spec(input.assignee_email)
            is_valid = (
                not remainder
                and parsed.addr_spec == input.assignee_email
                and all(isinstance(defect, NonASCIILocalPartDefect) for defect in parsed.all_defects)
            )
        except (ValueError, IndexError):
            is_valid = False
        if not is_valid:
            raise ValueError("assigneeEmail must be one bare email address")
    for field_name, names in (
        ("applyTagNames", input.apply_tag_names),
        ("removeTagNames", input.remove_tag_names),
    ):
        if len(names) > 20:
            raise ValueError(f"{field_name} holds more than 20 names")
        for index, name in enumerate(names):
            if (
                name.strip() != name
                or not name
                or len(name.encode()) > 100
                or any(character in name for character in "\0\r\n")
            ):
                raise ValueError(
                    f"{field_name}[{index}] must be a tag name of 1 to 100 bytes "
                    "without surrounding spaces or line breaks"
                )
    for index, name in enumerate(input.apply_tag_names):
        if name.casefold() in (candidate.casefold() for candidate in input.remove_tag_names):
            raise ValueError(f"applyTagNames[{index}] is also in removeTagNames")
    if not (input.status or input.assignee_email or input.apply_tag_names or input.remove_tag_names):
        raise ValueError(
            "at least one of status, assigneeEmail, applyTagNames, or removeTagNames is required"
        )


class UpdateConversationOperation:
    """Apply an absolute Hiver conversation change and read it back."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest's Mutation identity."""
        return UPDATE_CONVERSATION_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: UpdateConversationInput) -> IdempotencyKey:
        """Use the stable Call ID for receipt correlation."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: UpdateConversationInput
    ) -> MutationAttempt[UpdateConversationOutput]:
        """Resolve named resources, PATCH, and verify the resulting conversation."""
        empty = _empty_update()
        try:
            _validate_update(input)
        except ValueError as error:
            return _validation_mutation("updateConversation", empty, str(error))
        token = await self.client.token(call)
        if token is None:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                hiver_failure(
                    FailureKind.AUTHENTICATION, "updateConversation", "connection credentials are unavailable"
                ),
            )
        path = _conversation_path(input.inbox_id, input.conversation_id)
        assignee_id = ""
        applied: tuple[Tag, ...] = ()
        removed: tuple[Tag, ...] = ()
        if input.assignee_email:
            exchange = await self.client.exchange(
                call,
                token,
                "updateConversation",
                "GET",
                "/inboxes/" + path_escape(input.inbox_id) + "/users/search",
                {"email": input.assignee_email},
            )
            terminal = _mutation_result(call, self.client, exchange, empty, input.conversation_id)
            if terminal:
                return terminal
            try:
                users, _ = list_page(exchange.body)
                matches = list(
                    dict.fromkeys(
                        wire_id(user.get("id"))
                        for user in users
                        if string_field(user, "email").strip().casefold() == input.assignee_email.casefold()
                    )
                )
                for user in users:
                    validate_id("inbox user", wire_id(user.get("id")))
            except (ValueError, TypeError, AttributeError):
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    empty,
                    self.client.receipt(call, exchange, input.conversation_id),
                    hiver_failure(
                        FailureKind.PROTOCOL, "updateConversation", "Hiver returned an invalid inbox user"
                    ),
                )
            if len(matches) != 1:
                return MutationAttempt.branch_result(
                    "providerRejected",
                    empty,
                    self.client.receipt(call, exchange, input.conversation_id),
                    hiver_failure(
                        FailureKind.VALIDATION,
                        "updateConversation",
                        (
                            f"assigneeEmail matches {len(matches)} users of the shared inbox "
                            "instead of one; the change was not sent"
                        ),
                    ),
                )
            assignee_id = matches[0]
        if input.apply_tag_names or input.remove_tag_names:
            tags: list[Tag] = []
            page_token = ""
            complete = False
            for _ in range(5):
                exchange = await self.client.exchange(
                    call,
                    token,
                    "updateConversation",
                    "GET",
                    "/inboxes/" + path_escape(input.inbox_id) + "/tags",
                    page_query(page_token, 100),
                )
                terminal = _mutation_result(call, self.client, exchange, empty, input.conversation_id)
                if terminal:
                    return terminal
                try:
                    results, page_token = list_page(exchange.body)
                    tags.extend(decode_tag(raw) for raw in results)
                except (ValueError, TypeError, AttributeError) as error:
                    return MutationAttempt.branch_result(
                        "invalidResponse",
                        empty,
                        self.client.receipt(call, exchange, input.conversation_id),
                        hiver_failure(
                            FailureKind.PROTOCOL,
                            "updateConversation",
                            "Hiver returned an invalid tag page: " + str(error),
                        ),
                    )
                complete = not page_token
                if complete or all(
                    any(tag.name == name for tag in tags)
                    for name in (*input.apply_tag_names, *input.remove_tag_names)
                ):
                    break
            missing = (
                "is not a tag of the shared inbox"
                if complete
                else "is not among the tags on the first 5 pages Hiver returned"
            )
            try:
                applied = _match_tags("applyTagNames", input.apply_tag_names, tags, missing)
                removed = _match_tags("removeTagNames", input.remove_tag_names, tags, missing)
            except ValueError as error:
                return MutationAttempt.branch_result(
                    "providerRejected",
                    empty,
                    self.client.receipt(call, exchange, input.conversation_id),
                    hiver_failure(
                        FailureKind.VALIDATION, "updateConversation", str(error) + "; the change was not sent"
                    ),
                )
        change: dict[str, Any] = {}
        if input.status:
            change["status"] = {"name": "close" if input.status == "closed" else input.status}
        if input.assignee_email:
            change["assignee"] = {"email": input.assignee_email}
        if applied or removed:
            tag_change: dict[str, Any] = {}
            if applied:
                tag_change["to_apply"] = [tag.id for tag in applied]
            if removed:
                tag_change["to_remove"] = [tag.id for tag in removed]
            change["tags"] = tag_change
        exchange = await self.client.exchange(
            call, token, "updateConversation", "PATCH", path, payload=change
        )
        terminal = _mutation_result(call, self.client, exchange, empty, input.conversation_id)
        if terminal:
            return terminal
        exchange = await self.client.exchange(call, token, "updateConversation", "GET", path)
        terminal = _mutation_result(call, self.client, exchange, empty, input.conversation_id)
        if terminal:
            if terminal.kind == "branch" and terminal.branch == "providerRejected":
                problem = exchange.failure
                assert problem is not None
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    empty,
                    self.client.receipt(call, exchange, input.conversation_id),
                    hiver_failure(
                        problem.kind,
                        "updateConversation",
                        "Hiver accepted the change but rejected the read-back: " + problem.message,
                    ),
                )
            return terminal
        try:
            details = decode_conversation(single_data(exchange.body), input.inbox_id)
        except (ValueError, TypeError, AttributeError) as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                self.client.receipt(call, exchange, input.conversation_id),
                hiver_failure(
                    FailureKind.PROTOCOL,
                    "updateConversation",
                    "Hiver accepted the change but returned an invalid conversation: " + str(error),
                ),
            )
        conversation = details.conversation
        output = UpdateConversationOutput(conversation, assignee_id, applied, removed)
        missing_change = ""
        if input.status and conversation.status != input.status:
            missing_change = "status"
        elif assignee_id and (conversation.assignee is None or conversation.assignee.id != assignee_id):
            missing_change = "assignee"
        elif any(tag.id not in conversation.tag_ids for tag in applied):
            missing_change = "applied tags"
        elif any(tag.id in conversation.tag_ids for tag in removed):
            missing_change = "removed tags"
        if missing_change:
            return MutationAttempt.retry(
                hiver_failure(
                    FailureKind.CONFLICT,
                    "updateConversation",
                    "Hiver accepted the change but the conversation read back does not show its "
                    + missing_change
                    + " yet",
                )
            )
        return MutationAttempt.branch_result(
            "updated", output, self.client.receipt(call, exchange, conversation.id)
        )


def _match_tags(field_name: str, names: tuple[str, ...], tags: list[Tag], missing: str) -> tuple[Tag, ...]:
    matched = []
    problems = []
    for index, name in enumerate(names):
        candidates = [tag for tag in tags if tag.name == name]
        if not candidates:
            candidates = [tag for tag in tags if tag.name.casefold() == name.casefold()]
        candidates = list({tag.id: tag for tag in candidates}.values())
        if len(candidates) == 1:
            if candidates[0] not in matched:
                matched.append(candidates[0])
        elif not candidates:
            problems.append(f"{field_name}[{index}] {missing}")
        else:
            problems.append(f"{field_name}[{index}] matches several tags of the shared inbox")
    if problems:
        raise ValueError(", ".join(problems))
    return tuple(matched)


async def _fenced_attempt(
    client: Client,
    call: Call,
    operation: str,
    method: str,
    path: str,
    fields: list[tuple[str, str]],
    empty: Any,
    decode: Callable[[bytes], tuple[Any, str]],
    success_branch: str,
    validate: Callable[[], None],
) -> MutationAttempt[Any]:
    try:
        fence = Fence(call, timedelta(seconds=30), write_window_fraction=5 / 6)
        decision = await fence.decide()
    except ValueError:
        return MutationAttempt.branch_result(
            "defect",
            empty,
            Receipt(),
            hiver_failure(FailureKind.LOCAL_DEFECT, operation, "single-dispatch fence could not start"),
        )
    if decision.kind in (DecisionKind.READ_BACK_REQUIRED, DecisionKind.FAIL_CLOSED):
        return MutationAttempt.uncertain(
            empty,
            hiver_failure(
                FailureKind.TRANSPORT,
                operation,
                "an earlier attempt may have sent the request, so it is not sent again",
            ),
            client.receipt(call),
        )
    try:
        validate()
    except ValueError as error:
        return _validation_mutation(operation, empty, str(error))
    token = await client.token(call)
    if token is None:
        problem = hiver_failure(
            FailureKind.AUTHENTICATION, operation, "connection credentials are unavailable"
        )
        try:
            await fence.record_not_sent()
        except Exception:
            problem = hiver_failure(
                FailureKind.AVAILABILITY, operation, "Dex did not record the not-sent checkpoint"
            )
        return MutationAttempt.retry(problem)
    remaining = (fence.write_deadline - datetime.now(UTC)).total_seconds() * 0.6
    try:
        await asyncio.wait_for(client._wait_for_slot(), timeout=max(remaining, 0))
    except TimeoutError:
        problem = hiver_failure(
            FailureKind.AVAILABILITY,
            operation,
            "the wait for the connection's next Hiver request slot ended; no request was sent",
        )
        try:
            await fence.record_not_sent()
        except Exception:
            problem = hiver_failure(
                FailureKind.AVAILABILITY, operation, "Dex did not record the not-sent checkpoint"
            )
        return MutationAttempt.retry(problem)
    try:
        exchange = await fence.dispatch(
            lambda _deadline: client._send(call, token, operation, method, path, None, None, fields, True)
        )
    except (CheckpointError, InsufficientAttemptTimeError):
        return MutationAttempt.retry(
            hiver_failure(FailureKind.AVAILABILITY, operation, "Hiver write was not dispatched")
        )
    except (DispatchNotAllowedError, TimeoutError):
        return MutationAttempt.uncertain(
            empty,
            hiver_failure(FailureKind.AVAILABILITY, operation, "Hiver write was not dispatched"),
            client.receipt(call),
        )
    receipt = client.receipt(call, exchange)
    if exchange.outcome == "success":
        try:
            output, object_id = decode(exchange.body)
        except (ValueError, TypeError, AttributeError) as error:
            noun = "note" if operation == "addNote" else "draft"
            description = "note" if operation == "addNote" else "shared draft"
            return MutationAttempt.uncertain(
                empty,
                hiver_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    f"Hiver accepted the {noun} but returned an invalid {description}: {error}",
                ),
                receipt,
            )
        try:
            await fence.record_provider_id(object_id)
        except Exception:
            # The accepted response already proves success; this checkpoint only helps later recovery.
            pass
        return MutationAttempt.branch_result(
            success_branch, output, client.receipt(call, exchange, object_id)
        )
    assert exchange.failure is not None
    if exchange.outcome in ("rateLimited", "notSent") or (
        exchange.outcome == "unavailable" and exchange.status == 503
    ):
        try:
            await fence.record_can_resend()
        except Exception:
            return MutationAttempt.uncertain(
                empty,
                hiver_failure(
                    FailureKind.AVAILABILITY, operation, "Dex did not record the safe retry checkpoint"
                ),
                receipt,
            )
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    if exchange.outcome in ("unavailable", "invalid") or (
        exchange.outcome == "rejected" and exchange.status == 409
    ):
        return MutationAttempt.uncertain(empty, exchange.failure, receipt)
    route = {"notFound": "notFound", "defect": "defect"}.get(exchange.outcome, "providerRejected")
    return MutationAttempt.branch_result(route, empty, receipt, exchange.failure)


class AddNoteOperation:
    """Add one internal note through a single-dispatch fence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest's Mutation identity."""
        return ADD_NOTE_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: AddNoteInput) -> IdempotencyKey:
        """Use the stable Call ID for receipt correlation."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: AddNoteInput) -> MutationAttempt[AddNoteOutput]:
        """Send one multipart note when its fence permits a dispatch."""
        empty = AddNoteOutput()
        return await _fenced_attempt(
            self.client,
            call,
            "addNote",
            "POST",
            _conversation_path(input.inbox_id, input.conversation_id) + "/notes",
            [("content", input.content)],
            empty,
            _decode_add_note_output,
            "added",
            lambda: _validate_add_note(input),
        )


def _validate_add_note(input: AddNoteInput) -> None:
    validate_id("inboxId", input.inbox_id)
    validate_id("conversationId", input.conversation_id)
    _validate_text("content", input.content, 65536)


def _decode_add_note_output(body: bytes) -> tuple[AddNoteOutput, str]:
    note = decode_note(body)
    return AddNoteOutput(note), note.id


class CreateSharedDraftOperation:
    """Create one shared reply draft through a single-dispatch fence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest's Mutation identity."""
        return CREATE_SHARED_DRAFT_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: CreateSharedDraftInput) -> IdempotencyKey:
        """Use the stable Call ID for receipt correlation."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreateSharedDraftInput) -> MutationAttempt[SharedDraft]:
        """Send one multipart shared draft when its fence permits a dispatch."""
        empty = SharedDraft()
        fields = (
            [("body", input.body), ("hiver_message_id", input.hiver_message_id)]
            if input.hiver_message_id
            else [("body", input.body), ("gmail_message_id", input.gmail_message_id)]
        )
        return await _fenced_attempt(
            self.client,
            call,
            "createSharedDraft",
            "POST",
            "/inboxes/" + path_escape(input.inbox_id) + "/conversations/shared-drafts",
            fields,
            empty,
            _decode_shared_draft_output,
            "created",
            lambda: _validate_create_shared_draft(input),
        )


def _validate_create_shared_draft(input: CreateSharedDraftInput) -> None:
    validate_id("inboxId", input.inbox_id)
    if bool(input.hiver_message_id) == bool(input.gmail_message_id):
        raise ValueError("exactly one of hiverMessageId and gmailMessageId is required")
    if input.hiver_message_id:
        validate_id("hiverMessageId", input.hiver_message_id)
    elif not re.fullmatch(
        r"[A-Za-z0-9]{1,64}|<[\x21-\x3B\x3D\x3F-\x7E]{1,994}>",
        input.gmail_message_id,
    ):
        raise ValueError(
            "gmailMessageId must be a Gmail message ID or an SMTP Message-ID such as <abc123@mail.gmail.com>"
        )
    _validate_text("body", input.body, 65536)


def _decode_shared_draft_output(body: bytes) -> tuple[SharedDraft, str]:
    draft = decode_draft(body)
    return draft, draft.id
