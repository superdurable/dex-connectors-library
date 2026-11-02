# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Trello operation invocations and provider outcome routing."""

import json
import math
from datetime import timedelta
from typing import Any

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
from dex_connectors.sdk.providerhttp import ProviderTransportError
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions as definitions
from .client import (
    DETAIL_FIELDS,
    SUMMARY_FIELDS,
    Client,
    ProviderResponse,
    classify_response,
    decode_card,
    failure,
    parse_time,
    path,
    trello_id,
    trello_ids,
    trello_time,
)
from .models import (
    AddCommentInput,
    AddCommentOutput,
    Card,
    CreateCardInput,
    CreateCardOutput,
    GetCardInput,
    ListCardsInput,
    ListCardsOutput,
    UpdateCardInput,
    UpdateCardOutput,
)


def _validate_text(value: str, field: str, required: bool = False) -> None:
    if required and not value.strip():
        raise ValueError(f"{field} is required")
    if len(value) > 16384:
        raise ValueError(f"{field} cannot exceed 16384 characters")
    if any(
        (ord(character) < 32 and character not in "\n\r\t") or ord(character) == 127 for character in value
    ):
        raise ValueError(f"{field} cannot contain control characters")


def _position(value: str) -> str | float | None:
    value = value.strip()
    if not value:
        return None
    if value in ("top", "bottom"):
        return value
    try:
        number = float(value)
    except ValueError:
        number = 0
    if not math.isfinite(number) or number <= 0:
        raise ValueError("position must be top, bottom, or a positive number such as 16384")
    return number


def _read_attempt(
    operation: str, subject: str, response: ProviderResponse, empty: Any, call: Call, object_id: str = ""
) -> QueryAttempt[Any] | None:
    route, reason, delay = classify_response(operation, subject, response)
    if route == "accepted":
        return None
    if route == "retry":
        assert reason is not None
        return QueryAttempt.retry(reason, delay)
    assert reason is not None
    return QueryAttempt.branch_result(route, empty, call_receipt(call, response, object_id), reason)


def call_receipt(call: Call, response: ProviderResponse | None = None, object_id: str = "") -> Receipt:
    """Create a receipt using the provider's bounded correlation header."""
    import re
    from datetime import UTC, datetime

    request_id = response.headers.get("Atl-Request-Id", "") if response else ""
    if re.fullmatch(r"[A-Za-z0-9-]{1,64}", request_id) is None:
        request_id = ""
    return Receipt(call.id, "trello", datetime.now(UTC), call.idempotency_key, object_id, request_id)


async def _get_card(
    client: Client, call: Call, credentials: Any, card_id: str, operation: str
) -> tuple[Card | None, ProviderResponse | None, str, Failure | None, float]:
    query = {"fields": DETAIL_FIELDS, "members": "true", "member_fields": "fullName,username", "list": "true"}
    try:
        response = await client.request(credentials, "GET", path("cards", card_id), query)
    except ProviderTransportError:
        return (
            None,
            None,
            "retry",
            failure(operation, FailureKind.TRANSPORT, "Trello card is temporarily unavailable"),
            0,
        )
    route, reason, delay = classify_response(operation, "card", response)
    if route != "accepted":
        return None, response, route, reason, delay
    try:
        card = decode_card(json.loads(response.body), True)
        if card.id.lower() != card_id.lower():
            raise ValueError("response describes another card")
        return card, response, "accepted", None, 0
    except (ValueError, TypeError, KeyError, json.JSONDecodeError) as error:
        return (
            None,
            response,
            "invalidResponse",
            failure(operation, FailureKind.PROTOCOL, f"Trello returned an invalid card: {error}"),
            0,
        )


class ListCardsOperation:
    """Read a bounded page from a Trello board or list."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated listCards identity and retry policy."""
        return definitions.LIST_CARDS_DEFINITION

    async def invoke(self, call: Call, input: ListCardsInput) -> QueryAttempt[ListCardsOutput]:
        """Validate filters, read one page, and route a definite result."""
        empty = ListCardsOutput()
        try:
            board_id = trello_id(input.board_id, "boardId") if input.board_id.strip() else ""
            list_id = trello_id(input.list_id, "listId") if input.list_id.strip() else ""
            if bool(board_id) == bool(list_id):
                raise ValueError("set exactly one of boardId and listId")
            status = input.status or "open"
            if status not in ("open", "closed", "all"):
                raise ValueError("status must be open, closed, or all")
            if (input.due_before or input.due_after) and input.has_due_date is False:
                raise ValueError(
                    "dueBefore and dueAfter need cards with a due date, so hasDueDate cannot be false"
                )
            if input.due_before and input.due_after and input.due_after >= input.due_before:
                raise ValueError("dueAfter must be earlier than dueBefore")
            size = input.page_size or 50
            if not 1 <= size <= 100:
                raise ValueError("pageSize must be between 1 and 100")
            before = trello_id(input.before, "before") if input.before.strip() else ""
            query = {"filter": status, "fields": SUMMARY_FIELDS, "limit": str(size), "sort": "-id"}
            if before:
                query["before"] = before
            request_path = path("boards" if board_id else "lists", board_id or list_id, "cards")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure("listCards", FailureKind.VALIDATION, str(error))
            )
        try:
            credentials = await self.client.resolve_credentials(call)
        except Exception:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    "listCards",
                    FailureKind.AUTHENTICATION,
                    (
                        "Trello connection credentials are unavailable; "
                        "save an API key and a token for the connection"
                    ),
                ),
            )
        try:
            response = await self.client.request(credentials, "GET", request_path, query)
        except ProviderTransportError:
            return QueryAttempt.retry(
                failure("listCards", FailureKind.TRANSPORT, "Trello card list is temporarily unavailable")
            )
        routed = _read_attempt("listCards", "card list", response, empty, call)
        if routed is not None:
            return routed
        try:
            resources = json.loads(response.body)
            if not isinstance(resources, list):
                raise ValueError("response must be a JSON array")
            if len(resources) > size:
                raise ValueError("page holds more cards than requested")
            cards = tuple(decode_card(resource, False) for resource in resources)
            matching = tuple(
                card
                for card in cards
                if (input.has_due_date is None or input.has_due_date == (card.due is not None))
                and (input.is_due_complete is None or input.is_due_complete == card.is_due_complete)
                and (input.due_before is None or card.due is not None and card.due < input.due_before)
                and (input.due_after is None or card.due is not None and card.due >= input.due_after)
            )
            oldest = min((card.id for card in cards), key=str.lower, default="") if len(cards) == size else ""
            output = ListCardsOutput(matching, len(cards), oldest)
        except (ValueError, TypeError, KeyError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                call_receipt(call, response),
                failure("listCards", FailureKind.PROTOCOL, f"Trello returned an invalid card page: {error}"),
            )
        return QueryAttempt.branch_result("listed", output, call_receipt(call, response))


class GetCardOperation:
    """Read one Trello card with detail fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated getCard identity and retry policy."""
        return definitions.GET_CARD_DEFINITION

    async def invoke(self, call: Call, input: GetCardInput) -> QueryAttempt[Card]:
        """Read and validate a card from Trello."""
        empty = Card()
        try:
            card_id = trello_id(input.card_id, "cardId")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure("getCard", FailureKind.VALIDATION, str(error))
            )
        try:
            credentials = await self.client.resolve_credentials(call)
        except Exception:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    "getCard",
                    FailureKind.AUTHENTICATION,
                    (
                        "Trello connection credentials are unavailable; "
                        "save an API key and a token for the connection"
                    ),
                ),
            )
        card, response, route, reason, delay = await _get_card(
            self.client, call, credentials, card_id, "getCard"
        )
        if route == "retry":
            assert reason is not None
            return QueryAttempt.retry(reason, delay)
        if route != "accepted":
            assert reason is not None
            return QueryAttempt.branch_result(route, empty, call_receipt(call, response, card_id), reason)
        assert card is not None
        return QueryAttempt.branch_result("found", card, call_receipt(call, response, card_id))


async def _single_dispatch(
    client: Client,
    call: Call,
    credentials: Any,
    operation: str,
    subject: str,
    request_path: str,
    payload: dict[str, Any],
    requested: Any,
    not_found: bool = False,
) -> tuple[ProviderResponse | None, MutationAttempt[Any] | None]:
    receipt = call_receipt(call, object_id=request_path.split("/")[2] if not_found else "")
    if call.context is None:
        return None, MutationAttempt.retry(
            failure(operation, FailureKind.AVAILABILITY, "Dex dispatch context is unavailable")
        )
    fence = Fence(call, timedelta(seconds=30))
    decision = await fence.decide()
    if decision.kind != DecisionKind.SEND_ALLOWED:
        return None, MutationAttempt.uncertain(
            requested,
            failure(
                operation,
                FailureKind.TRANSPORT,
                "an earlier attempt of this Step may have sent the request, so it is not sent again",
            ),
            receipt,
        )
    try:
        response = await fence.dispatch(
            lambda deadline: client.request(credentials, "POST", request_path, payload=payload)
        )
    except (CheckpointError, InsufficientAttemptTimeError):
        return None, MutationAttempt.retry(
            failure(
                operation,
                FailureKind.AVAILABILITY,
                "Dex did not record the dispatch checkpoint, so nothing was sent to Trello",
            )
        )
    except ProviderTransportError as error:
        if error.provably_not_sent:
            await fence.record_can_resend()
            return None, MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    f"Trello could not be reached, so no {subject} was written",
                )
            )
        return None, MutationAttempt.uncertain(
            requested,
            failure(operation, FailureKind.TRANSPORT, f"Trello {subject} outcome is unknown"),
            receipt,
        )
    route, reason, delay = classify_response(operation, subject, response, write=True)
    if route == "retry":
        await fence.record_can_resend()
        assert reason is not None
        return response, MutationAttempt.retry(reason, delay)
    if route == "uncertain":
        assert reason is not None
        return response, MutationAttempt.uncertain(
            requested, reason, call_receipt(call, response, receipt.provider_object_id)
        )
    if route in ("providerRejected", "notFound"):
        assert reason is not None
        return response, MutationAttempt.branch_result(
            "notFound" if not_found and route == "notFound" else "providerRejected",
            requested,
            call_receipt(call, response, receipt.provider_object_id),
            reason,
        )
    return response, None


class CreateCardOperation:
    """Create a Trello card once after a durable Dex dispatch claim."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated createCard identity and retry policy."""
        return definitions.CREATE_CARD_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateCardInput) -> IdempotencyKey:
        """Retain Call ID as receipt identity; Trello receives no key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateCardInput) -> MutationAttempt[CreateCardOutput]:
        """Send a single card creation and preserve uncertain outcomes."""
        requested = CreateCardOutput(name=input.name.strip(), list_id=input.list_id.strip())
        try:
            list_id = trello_id(input.list_id, "listId")
            name = input.name.strip()
            if not name:
                raise ValueError("name is required")
            _validate_text(name, "name")
            if "\n" in name or "\r" in name or "\t" in name:
                raise ValueError("name must be one line without control characters")
            _validate_text(input.description, "description")
            label_ids = trello_ids(input.label_ids, "labelIds")
            member_ids = trello_ids(input.member_ids, "memberIds")
            position = _position(input.position)
            payload: dict[str, Any] = {"idList": list_id, "name": name}
            if input.description.strip():
                payload["desc"] = input.description
            if input.due is not None:
                payload["due"] = trello_time(input.due)
            if label_ids:
                payload["idLabels"] = ",".join(label_ids)
            if member_ids:
                payload["idMembers"] = ",".join(member_ids)
            if position is not None:
                payload["pos"] = position
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure("createCard", FailureKind.VALIDATION, str(error))
            )
        try:
            credentials = await self.client.resolve_credentials(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                requested,
                Receipt(),
                failure(
                    "createCard",
                    FailureKind.AUTHENTICATION,
                    (
                        "Trello connection credentials are unavailable; "
                        "save an API key and a token for the connection"
                    ),
                ),
            )
        response, routed = await _single_dispatch(
            self.client, call, credentials, "createCard", "card", "/cards", payload, requested
        )
        if routed is not None:
            return routed
        assert response is not None
        try:
            resource = json.loads(response.body)
            if not isinstance(resource, dict):
                raise ValueError("card response must be an object")
            card_id = trello_id(resource.get("id", ""), "cardId")
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "createCard",
                    FailureKind.PROTOCOL,
                    "Trello accepted the card but returned an unusable card reference",
                ),
                call_receipt(call, response),
            )
        output = CreateCardOutput(
            card_id,
            requested.name,
            requested.list_id,
            resource.get("idBoard", "")
            if isinstance(resource.get("idBoard"), str) and len(resource["idBoard"]) == 24
            else "",
            resource.get("shortLink", "")
            if isinstance(resource.get("shortLink"), str) and len(resource["shortLink"]) == 8
            else "",
            resource.get("url", ""),
            resource.get("shortUrl", ""),
        )
        return MutationAttempt.branch_result("created", output, call_receipt(call, response, card_id))


class AddCommentOperation:
    """Add a Trello comment once after a durable Dex dispatch claim."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated addComment identity and retry policy."""
        return definitions.ADD_COMMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddCommentInput) -> IdempotencyKey:
        """Retain Call ID as receipt identity; Trello receives no key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AddCommentInput) -> MutationAttempt[AddCommentOutput]:
        """Send one comment and route ambiguous outcomes to uncertainty."""
        requested = AddCommentOutput(card_id=input.card_id.strip())
        try:
            card_id = trello_id(input.card_id, "cardId")
            _validate_text(input.text, "text", True)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure("addComment", FailureKind.VALIDATION, str(error))
            )
        try:
            credentials = await self.client.resolve_credentials(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                requested,
                Receipt(),
                failure(
                    "addComment",
                    FailureKind.AUTHENTICATION,
                    (
                        "Trello connection credentials are unavailable; "
                        "save an API key and a token for the connection"
                    ),
                ),
            )
        response, routed = await _single_dispatch(
            self.client,
            call,
            credentials,
            "addComment",
            "comment",
            path("cards", card_id, "actions", "comments"),
            {"text": input.text},
            requested,
            True,
        )
        if routed is not None:
            return routed
        assert response is not None
        try:
            resource = json.loads(response.body)
            if not isinstance(resource, dict):
                raise ValueError("comment response must be an object")
            comment_id = trello_id(resource.get("id", ""), "commentId")
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "addComment",
                    FailureKind.PROTOCOL,
                    "Trello accepted the comment but returned an unusable action reference",
                ),
                call_receipt(call, response, card_id),
            )
        author_id = resource.get("idMemberCreator", "")
        if not isinstance(author_id, str) or len(author_id) != 24:
            author_id = ""
        try:
            created_at = parse_time(resource.get("date"))
        except ValueError:
            created_at = None
        output = AddCommentOutput(card_id, comment_id, author_id, created_at)
        return MutationAttempt.branch_result("added", output, call_receipt(call, response, comment_id))


class UpdateCardOperation:
    """Apply absolute Trello card changes and confirm them with a read-back."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated updateCard identity and retry policy."""
        return definitions.UPDATE_CARD_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateCardInput) -> IdempotencyKey:
        """Retain Call ID in the receipt; absolute values need no provider key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateCardInput) -> MutationAttempt[UpdateCardOutput]:
        """Read labels if needed, send one repeatable PUT, and verify the result."""
        output = UpdateCardOutput(card_id=input.card_id.strip())
        try:
            card_id = trello_id(input.card_id, "cardId")
            fields: dict[str, Any] = {}
            if input.list_id.strip():
                fields["idList"] = trello_id(input.list_id, "listId")
            if input.board_id.strip():
                if not input.list_id.strip():
                    raise ValueError("boardId requires listId, the list on that board that receives the card")
                fields["idBoard"] = trello_id(input.board_id, "boardId")
            position = _position(input.position)
            if position is not None:
                fields["pos"] = position
            if input.is_closed is not None:
                fields["closed"] = input.is_closed
            if input.due is not None and input.should_clear_due:
                raise ValueError("set at most one of due and shouldClearDue")
            if input.due is not None:
                fields["due"] = trello_time(input.due)
            elif input.should_clear_due:
                fields["due"] = None
            if input.is_due_complete is not None:
                fields["dueComplete"] = input.is_due_complete
            add_ids = trello_ids(input.add_label_ids, "addLabelIds")
            remove_ids = trello_ids(input.remove_label_ids, "removeLabelIds")
            if {item.lower() for item in add_ids} & {item.lower() for item in remove_ids}:
                raise ValueError("a label is both added and removed")
            if input.label_ids is not None and (add_ids or remove_ids):
                raise ValueError("set either labelIds or addLabelIds and removeLabelIds, not both")
            if input.label_ids is not None:
                fields["idLabels"] = ",".join(trello_ids(input.label_ids, "labelIds"))
            if input.member_ids is not None:
                fields["idMembers"] = ",".join(trello_ids(input.member_ids, "memberIds"))
            if not fields and not add_ids and not remove_ids:
                raise ValueError(
                    "set at least one of listId, position, isClosed, due, shouldClearDue, "
                    "isDueComplete, labelIds, addLabelIds, removeLabelIds, and memberIds"
                )
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", output, Receipt(), failure("updateCard", FailureKind.VALIDATION, str(error))
            )
        try:
            credentials = await self.client.resolve_credentials(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                output,
                Receipt(),
                failure(
                    "updateCard",
                    FailureKind.AUTHENTICATION,
                    (
                        "Trello connection credentials are unavailable; "
                        "save an API key and a token for the connection"
                    ),
                ),
            )
        if add_ids or remove_ids:
            current, read_response, route, reason, delay = await _get_card(
                self.client, call, credentials, card_id, "updateCard"
            )
            if route != "accepted":
                return _update_read_result(call, output, read_response, route, reason, delay, card_id)
            assert current is not None
            if current.has_more_labels:
                return MutationAttempt.branch_result(
                    "defect",
                    output,
                    call_receipt(call, read_response, card_id),
                    failure(
                        "updateCard",
                        FailureKind.VALIDATION,
                        (
                            "the card carries more than 50 labels, so set labelIds "
                            "with the complete list instead"
                        ),
                    ),
                )
            labels = [
                item
                for item in current.label_ids
                if item.lower() not in {value.lower() for value in remove_ids}
            ]
            for label_id in add_ids:
                if label_id.lower() not in {value.lower() for value in labels}:
                    labels.append(label_id)
            if len(labels) > 50:
                return MutationAttempt.branch_result(
                    "defect",
                    output,
                    call_receipt(call, read_response, card_id),
                    failure("updateCard", FailureKind.VALIDATION, "the card would carry more than 50 labels"),
                )
            fields["idLabels"] = ",".join(labels)
        try:
            response = await self.client.request(credentials, "PUT", path("cards", card_id), payload=fields)
        except ProviderTransportError:
            return MutationAttempt.retry(
                failure(
                    "updateCard",
                    FailureKind.TRANSPORT,
                    "Trello card update outcome is unknown, so the update is sent again",
                )
            )
        route, reason, delay = classify_response(
            "updateCard", "card update", response, write=True, repeatable=True
        )
        if route != "accepted":
            if route == "retry":
                assert reason is not None
                return MutationAttempt.retry(reason, delay)
            assert reason is not None
            return MutationAttempt.branch_result(route, output, call_receipt(call, response, card_id), reason)
        card, read_response, route, reason, delay = await _get_card(
            self.client, call, credentials, card_id, "updateCard"
        )
        if route != "accepted":
            return _update_read_result(call, output, read_response, route, reason, delay, card_id)
        assert card is not None
        wrong = _first_unapplied_field(input, fields, card)
        if wrong:
            return MutationAttempt.branch_result(
                "invalidResponse",
                output,
                call_receipt(call, read_response, card_id),
                failure(
                    "updateCard",
                    FailureKind.PROTOCOL,
                    f"Trello accepted the update but the card read back does not show the requested {wrong}",
                ),
            )
        return MutationAttempt.branch_result(
            "updated", UpdateCardOutput(card_id, card), call_receipt(call, read_response, card_id)
        )


def _update_read_result(
    call: Call,
    output: UpdateCardOutput,
    response: ProviderResponse | None,
    route: str,
    reason: Failure | None,
    delay: float,
    card_id: str,
) -> MutationAttempt[UpdateCardOutput]:
    assert reason is not None
    if route == "retry":
        return MutationAttempt.retry(reason, delay)
    return MutationAttempt.branch_result(route, output, call_receipt(call, response, card_id), reason)


def _first_unapplied_field(input: UpdateCardInput, fields: dict[str, Any], card: Card) -> str:
    if "idList" in fields and card.list_id.lower() != str(fields["idList"]).lower():
        return "list"
    if "idBoard" in fields and card.board_id.lower() != str(fields["idBoard"]).lower():
        return "board"
    if input.is_closed is not None and card.is_closed != input.is_closed:
        return "archive state"
    if input.due is not None and (card.due is None or trello_time(card.due) != trello_time(input.due)):
        return "due date"
    if input.should_clear_due and card.due is not None:
        return "cleared due date"
    if input.is_due_complete is not None and card.is_due_complete != input.is_due_complete:
        return "due-complete state"
    if "idLabels" in fields and (
        card.has_more_labels
        or {item.lower() for item in card.label_ids}
        != {item.lower() for item in str(fields["idLabels"]).split(",") if item}
    ):
        return "labels"
    if "idMembers" in fields and (
        {item.lower() for item in card.member_ids}
        != {item.lower() for item in str(fields["idMembers"]).split(",") if item}
    ):
        return "members"
    return ""
