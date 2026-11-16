# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll one Trello board's card action history for the cardChanged Trigger."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk import ConnectionRef, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.providerhttp import ProviderTransportError

from ._generated.definitions import CARD_CHANGED_TRIGGER_DEFINITION
from .card_action_cursor import (
    ACTION_LOOKBACK_NANOSECONDS,
    CardActionCursor,
    GoTime,
    decode_card_action_cursor,
    encode_card_action_cursor,
    format_trello_time,
    parse_go_time,
    seen_action_ids_within,
)
from .client import Client, ProviderResponse, classify_response, path
from .models import ActionCard, ActionList, CardChangedEvent, CardChangedTriggerConfiguration, MemberReference

CARD_ACTION_CREATED: Final = "createCard"
"""A card was added to a list on the board."""
CARD_ACTION_COPIED: Final = "copyCard"
"""A card was created as a copy of another card."""
CARD_ACTION_CONVERTED_FROM_CHECK_ITEM: Final = "convertToCardFromCheckItem"
"""A checklist item became a card."""
CARD_ACTION_EMAILED: Final = "emailCard"
"""A card was created by email to the board."""
CARD_ACTION_UPDATED: Final = "updateCard"
"""A field changed, such as the list (a move), name, due date, or closed; changed_fields names them."""
CARD_ACTION_COMMENTED: Final = "commentCard"
"""A member commented on a card; comment_text holds the comment."""
CARD_ACTION_MOVED_TO_BOARD: Final = "moveCardToBoard"
"""A card arrived from another board."""
CARD_ACTION_DELETED: Final = "deleteCard"
"""A card was deleted; Trello keeps only its ID, short ID, and list."""
WATCHED_CARD_ACTION_TYPES: Final = (
    CARD_ACTION_CREATED,
    CARD_ACTION_COPIED,
    CARD_ACTION_CONVERTED_FROM_CHECK_ITEM,
    CARD_ACTION_EMAILED,
    CARD_ACTION_UPDATED,
    CARD_ACTION_COMMENTED,
    CARD_ACTION_MOVED_TO_BOARD,
    CARD_ACTION_DELETED,
)
"""The Trello action types every cardChanged poll requests, in Go's filter order."""

_OPERATION: Final = "cardChanged"
_SUBJECT: Final = "board action history"
_PAGE_SIZE: Final = MAXIMUM_CHANGES_PER_PAGE
# Bounds one walk back through the history to 10,000 actions.
_MAXIMUM_PAGES_PER_POLL: Final = 20
_TRELLO_ID: Final = re.compile(r"[0-9a-fA-F]{24}\Z", re.ASCII)
_SHORT_LINK: Final = re.compile(r"[0-9A-Za-z]{8}\Z", re.ASCII)
_INT64_MINIMUM: Final = -(2**63)
_INT64_MAXIMUM: Final = 2**63 - 1
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)


def validate_card_changed_trigger_configuration(configuration: CardChangedTriggerConfiguration) -> None:
    """Require a 24-character board ID or an 8-character short link without surrounding whitespace.

    Raises ValueError.
    """
    board_id = configuration.board_id
    if not _TRELLO_ID.match(board_id) and not _SHORT_LINK.match(board_id):
        raise ValueError(
            "cardChanged boardId must be a 24-character Trello board ID or an 8-character board short link"
        )


def card_change_event_id(action_id: str) -> str:
    """Return the Trigger event ID of one action, a pure function of its ID that survives a refetch."""
    return "trello-action-" + action_id.lower()


class _GoFloat(float):
    """A JSON number written with a fraction or exponent, which Go never decodes into an integer."""


@dataclass(frozen=True, slots=True)
class _CardAction:
    action_id: str
    action_type: str
    occurred_at: GoTime
    event: CardChangedEvent

    def order(self) -> tuple[int, str]:
        return self.occurred_at.unix_nanoseconds, self.action_id.lower()


class CardActionFetcher:
    """Read one board's card actions for the poll Trigger kit; owned by one poller at a time."""

    def __init__(
        self, client: Client, connection: ConnectionRef, configuration: CardChangedTriggerConfiguration
    ) -> None:
        """Bind the client and the watched board; validate configuration beforehand."""
        self._client = client
        self._call = new_trigger_credential_call(connection, CARD_CHANGED_TRIGGER_DEFINITION.trigger)
        self._board_id = configuration.board_id

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Read the newest page and keep only its position, so earlier actions are never delivered."""
        newest = await self._list_action_page(context, None, "")
        position = CardActionCursor(board_id=self._board_id)
        if newest:
            watermark = max(newest, key=_CardAction.order).occurred_at
            position.watermark = format_trello_time(watermark)
            position.seen_action_ids, position.floor_action_id = seen_action_ids_within(
                [], "", [action.action_id for action in newest], watermark
            )
        return encode_card_action_cursor(position)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[CardChangedEvent]]:
        """Walk the actions after the lookback window, drop delivered ones, and return the oldest page.

        An undecodable cursor, one for another board, a 400 naming since or before, or a backlog over
        10,000 actions is marked invalid, so the kit records a new baseline.
        """
        try:
            position = decode_card_action_cursor(cursor.token, self._board_id)
        except ValueError as error:
            raise mark_cursor_invalid(error) from None
        watermark = parse_go_time(position.watermark) if position.watermark else None
        since = None if watermark is None else watermark.unix_nanoseconds - ACTION_LOOKBACK_NANOSECONDS
        listed = await self._walk_actions_since(context, since)
        seen = set(position.seen_action_ids)
        fresh = [
            action
            for action in listed
            if action.action_id.lower() not in seen
            and action.action_id.lower() > position.floor_action_id
            and (since is None or action.occurred_at.unix_nanoseconds >= since)
        ]
        fresh.sort(key=_CardAction.order)
        has_more_pages = len(fresh) > _PAGE_SIZE
        fresh = fresh[:_PAGE_SIZE]
        if fresh:
            latest = max(fresh, key=_CardAction.order).occurred_at
            if watermark is None or not watermark.unix_nanoseconds > latest.unix_nanoseconds:
                watermark = latest
            position.watermark = format_trello_time(watermark)
        position.seen_action_ids, position.floor_action_id = seen_action_ids_within(
            position.seen_action_ids,
            position.floor_action_id,
            [action.action_id for action in fresh],
            watermark,
        )
        changes = [
            TriggerEvent(card_change_event_id(action.action_id), action.event.occurred_at, action.event)
            for action in fresh
        ]
        return ChangePage(changes, encode_card_action_cursor(position), has_more_pages)

    async def _walk_actions_since(self, context: TriggerContext, since: int | None) -> list[_CardAction]:
        collected: list[_CardAction] = []
        before = ""
        for _ in range(_MAXIMUM_PAGES_PER_POLL):
            listed = await self._list_action_page(context, since, before)
            collected.extend(listed)
            if len(listed) < _PAGE_SIZE:
                return collected
            # Trello compares before against action IDs, so page from the smallest ID.
            oldest = min(listed, key=lambda action: action.action_id.lower()).action_id
            if oldest == before:
                raise RuntimeError("Trello actions page did not move past its before cursor")
            before = oldest
        raise mark_cursor_invalid(
            RuntimeError(
                f"Trello board has more than {_MAXIMUM_PAGES_PER_POLL * _PAGE_SIZE} card actions "
                "since the last poll"
            )
        )

    async def _list_action_page(
        self, context: TriggerContext, since: int | None, before: str
    ) -> list[_CardAction]:
        """Send one GET /boards/{board}/actions for the watched types, newest first."""
        await wait_for_poll_request(context)
        try:
            credentials = await self._client.resolve_credentials(self._call)
        except Exception:
            raise RuntimeError(
                "Trello cardChanged credentials are unavailable; "
                "save an API key and a token for the connection"
            ) from None
        query = {
            "fields": "id,idMemberCreator,type,date,data",
            "filter": ",".join(WATCHED_CARD_ACTION_TYPES),
            "limit": str(_PAGE_SIZE),
            "memberCreator": "true",
            "memberCreator_fields": "fullName,username",
        }
        if since is not None:
            query["since"] = format_trello_time(GoTime(since))
        if before:
            query["before"] = before
        try:
            response = await self._client.request(
                credentials, "GET", path("boards", self._board_id, "actions"), query
            )
        except ProviderTransportError:
            raise RuntimeError(f"Trello {_SUBJECT} is temporarily unavailable") from None
        _raise_for_failed_page(response)
        try:
            resources = json.loads(response.body, parse_float=_GoFloat, parse_constant=_reject_constant)
            if not isinstance(resources, list):
                raise ValueError
            listed = [_decode_action(resource) for resource in resources]
        except (ValueError, RecursionError):
            raise RuntimeError("Trello actions page is not a JSON array of actions") from None
        if len(listed) > _PAGE_SIZE:
            raise RuntimeError(
                f"Trello actions page has {len(listed)} actions, more than the requested {_PAGE_SIZE}"
            )
        return [_validate_action(action) for action in listed]


def _raise_for_failed_page(response: ProviderResponse) -> None:
    """Map a failed page to a poll error: 429 pauses the connection, a 400 naming since or before reseeds."""
    outcome, failure, delay = classify_response(_OPERATION, _SUBJECT, response)
    if outcome == "accepted" or failure is None:
        return
    error = RuntimeError(failure.message)
    if response.status == 429:
        # Trello sends no Retry-After, so the pause defaults to one rate-limit window.
        raise mark_rate_limited(error, timedelta(seconds=delay))
    body = response.body.lower()
    if outcome == "providerRejected" and response.status == 400 and (b"since" in body or b"before" in body):
        raise mark_cursor_invalid(error)
    raise error


def _reject_constant(value: str) -> Any:
    raise ValueError(f"{value} is not JSON")


def _field(document: dict[str, Any], name: str) -> Any:
    """Return the value Go's encoding/json assigns to the field: the last exact or case-folded key."""
    value = None
    folded = name.lower()
    for key, item in document.items():
        if key == name or (key.isascii() and key.lower() == folded):
            value = item
    return value


def _string(document: dict[str, Any], name: str) -> str:
    value = _field(document, name)
    if value is not None and not isinstance(value, str):
        raise ValueError(name)
    return value or ""


def _int64(document: dict[str, Any], name: str) -> int:
    value = _field(document, name)
    if value is None:
        return 0
    if isinstance(value, bool) or not isinstance(value, int) or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
        raise ValueError(name)
    return value


def _optional_bool(document: dict[str, Any], name: str) -> bool | None:
    value = _field(document, name)
    if value is not None and not isinstance(value, bool):
        raise ValueError(name)
    return value


def _object(document: dict[str, Any], name: str) -> dict[str, Any] | None:
    value = _field(document, name)
    if value is not None and not isinstance(value, dict):
        raise ValueError(name)
    return value


def _action_list(data: dict[str, Any], name: str) -> ActionList | None:
    resource = _object(data, name)
    if resource is None:
        return None
    list_id, list_name = _string(resource, "id"), _string(resource, "name")
    return ActionList(list_id, list_name) if list_id else None


@dataclass(frozen=True, slots=True)
class _ActionResource:
    action_id: str
    action_type: str
    date: str
    member_creator_id: str
    member_creator: MemberReference | None
    card: ActionCard | None
    board: tuple[str, str]
    lists: tuple[ActionList | None, ActionList | None, ActionList | None]
    changed_fields: tuple[str, ...] | None
    text: str


def _decode_action(resource: Any) -> _ActionResource:
    """Decode one action with Go's type rules; raises ValueError for a field of the wrong JSON type."""
    if resource is None:
        resource = {}
    if not isinstance(resource, dict):
        raise ValueError("action")
    data = _object(resource, "data") or {}
    card_resource = _object(data, "card")
    card = None
    if card_resource is not None:
        card = ActionCard(
            id=_string(card_resource, "id"),
            name=_string(card_resource, "name"),
            short_link=_string(card_resource, "shortLink"),
            short_id=_int64(card_resource, "idShort"),
            list_id=_string(card_resource, "idList"),
            is_closed=_optional_bool(card_resource, "closed"),
        )
    board = _object(data, "board")
    old = _object(data, "old")
    creator = _object(resource, "memberCreator")
    member = None
    if creator is not None:
        creator_id = _string(creator, "id")
        full_name, username = _string(creator, "fullName"), _string(creator, "username")
        member = MemberReference(creator_id, full_name, username) if creator_id else None
    return _ActionResource(
        action_id=_string(resource, "id"),
        action_type=_string(resource, "type"),
        date=_string(resource, "date"),
        member_creator_id=_string(resource, "idMemberCreator"),
        member_creator=member,
        card=card,
        board=(_string(board, "id"), _string(board, "name")) if board is not None else ("", ""),
        lists=(_action_list(data, "list"), _action_list(data, "listBefore"), _action_list(data, "listAfter")),
        changed_fields=tuple(sorted(old)) if old is not None else None,
        text=_string(data, "text"),
    )


def _validate_action(resource: _ActionResource) -> _CardAction:
    """Check the fields cardChanged relies on, as Go's cardActionResource.validate does."""
    action_id = resource.action_id
    if not _TRELLO_ID.match(action_id):
        raise RuntimeError("Trello actions page has an invalid action: action id is not a Trello ID")
    if resource.action_type not in WATCHED_CARD_ACTION_TYPES:
        raise RuntimeError(
            f"Trello actions page has an invalid action: action {action_id} has unrequested type "
            f"{json.dumps(resource.action_type)}"
        )
    occurred_at = parse_go_time(resource.date)
    if occurred_at is None:
        raise RuntimeError(
            f"Trello actions page has an invalid action: action {action_id} date is not an ISO 8601 time"
        )
    occurred_at = GoTime(occurred_at.unix_nanoseconds)
    if resource.card is None or not _TRELLO_ID.match(resource.card.id):
        raise RuntimeError(f"Trello actions page has an invalid action: action {action_id} has no card ID")
    board_id, board_name = resource.board
    listed, list_before, list_after = resource.lists
    event = CardChangedEvent(
        board_id=board_id,
        board_name=board_name,
        action_id=action_id,
        action_type=resource.action_type,
        occurred_at=_UNIX_EPOCH + timedelta(microseconds=occurred_at.unix_nanoseconds // 1000),
        member_creator_id=resource.member_creator_id,
        member_creator=resource.member_creator,
        card=resource.card,
        list=listed,
        list_before=list_before,
        list_after=list_after,
        changed_fields=(resource.changed_fields or ()) if resource.action_type == CARD_ACTION_UPDATED else (),
        comment_text=resource.text if resource.action_type == CARD_ACTION_COMMENTED else "",
    )
    return _CardAction(action_id, resource.action_type, occurred_at, event)
