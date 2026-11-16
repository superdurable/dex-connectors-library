# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll Re:amaze's GET /conversations by creation time for the conversationCreated Trigger.

The cursor token is the Go connector's JSON document byte for byte, so Go and Python replicas share one
binding cursor.
"""

import hashlib
import json
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk import (
    ConnectionRef,
    StandardLoggingTriggerLogger,
    TriggerContext,
    TriggerEvent,
    TriggerLogger,
    TriggerLogLevel,
)
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import encode_go_json_value

from ._generated.definitions import CONNECTOR_ID, CONVERSATION_CREATED_TRIGGER_DEFINITION
from .client import Client, CredentialsUnavailableError, CredentialsUnusableError
from .decoding import decode_conversation_page
from .models import (
    Conversation,
    ConversationCreatedEvent,
    ConversationCreatedTriggerConfiguration,
    SearchConversationsInput,
    SearchConversationsOutput,
)
from .operations import _search_query

CONVERSATION_CREATED_LATE_INDEX_WINDOW: Final = timedelta(minutes=10)
"""How far before the newest creation time a poll still accepts a conversation it has not seen.

Re:amaze lists a conversation some time after creating it.
"""
MAXIMUM_CONVERSATION_CREATED_PAGES_PER_POLL: Final = 16
"""Bounds the GET /conversations pages one poll reads, 480 conversations, below the kit's page bound."""

# Keeps the cursor token well below the kit's 64 KiB bound.
_MAXIMUM_SEEN_CONVERSATIONS: Final = 800
_MAXIMUM_SEARCH_PAGE: Final = 10000
_LATE_INDEX_WINDOW_MILLISECONDS: Final = 600_000
_OPERATION: Final = "conversationCreated"
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_INT64_MINIMUM, _INT64_MAXIMUM = -(1 << 63), (1 << 63) - 1
_TRIGGER_NAME: Final = CONVERSATION_CREATED_TRIGGER_DEFINITION.trigger.trigger_name


class ConversationCreatedCursorInvalidError(ValueError):
    """A conversationCreated cursor token that is not a valid Go cursor document."""

    def __init__(self) -> None:
        """Describe the rejected token without echoing it."""
        super().__init__("Re:amaze conversationCreated cursor is not decodable")


@dataclass(frozen=True, slots=True)
class _SeenConversation:
    # A slug digest bounds the entry size for any slug length.
    digest: str
    created_at_milliseconds: int


@dataclass(slots=True)
class _ConversationCreatedCursor:
    # Field order is the Go struct's, so the JSON encoding matches byte for byte.
    brand: str = ""
    channel: str = ""
    watermark_milliseconds: int = 0
    floor_milliseconds: int = 0
    seen: list[_SeenConversation] = field(default_factory=list)


@dataclass(slots=True)
class _ConversationWalk:
    conversations: list[Conversation] = field(default_factory=list)
    # Whether the walk read every conversation at or above the floor it was given.
    reaches_floor: bool = False
    pages_read: int = 0


def unix_milliseconds(moment: datetime) -> int:
    """Return moment's Unix time in whole milliseconds, rounded down as Go's Time.UnixMilli does."""
    return (moment - _UNIX_EPOCH) // timedelta(milliseconds=1)


def conversation_created_event_id(brand: str, slug: str) -> str:
    """Return the Trigger event ID, a hash of the brand and slug, so it stays short for any slug length.

    It is a pure function of the conversation, so a refetch after a crash yields the same ID.
    """
    digest = hashlib.sha256(f"{brand}\x00{slug}".encode()).hexdigest()
    return "reamaze-conversation-" + digest[:32]


def conversation_digest(slug: str) -> str:
    """Return the slug digest that the cursor's seen set stores."""
    return hashlib.sha256(slug.encode()).hexdigest()[:16]


def _new_conversation_created_trigger_event(
    brand: str, channel: str, conversation: Conversation
) -> TriggerEvent[ConversationCreatedEvent]:
    return TriggerEvent(
        conversation_created_event_id(brand, conversation.id),
        conversation.created_at,
        ConversationCreatedEvent(brand=brand, channel=channel, conversation=conversation),
    )


def _encode_conversation_created_cursor(position: _ConversationCreatedCursor) -> Cursor:
    """Encode the cursor as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings."""
    members = ['"brand":' + encode_go_json_value(position.brand).decode()]
    if position.channel:
        members.append('"channel":' + encode_go_json_value(position.channel).decode())
    members.append(f'"watermark":{position.watermark_milliseconds}')
    members.append(f'"floor":{position.floor_milliseconds}')
    seen = ",".join(
        '{"d":' + encode_go_json_value(entry.digest).decode() + f',"t":{entry.created_at_milliseconds}' + "}"
        for entry in position.seen
    )
    members.append('"seen":[' + seen + "]")
    return Cursor(token="{" + ",".join(members) + "}")


class _JSONObject(list[tuple[str, Any]]):
    """The members of one JSON object in document order, distinct from a JSON array."""


class _JSONFloat(str):
    """A JSON number with a fraction or exponent, which Go never decodes into an int64."""


def _reject_json_constant(constant: str) -> None:
    raise ConversationCreatedCursorInvalidError()


def _go_int64(value: Any) -> int:
    if type(value) is not int or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
        raise ConversationCreatedCursorInvalidError()
    return value


def _go_string(value: Any) -> str:
    if not isinstance(value, str) or isinstance(value, _JSONFloat):
        raise ConversationCreatedCursorInvalidError()
    return value


def _decode_seen_conversation(value: Any) -> _SeenConversation:
    if value is None:
        # Go leaves a null array element as the zero entry.
        return _SeenConversation("", 0)
    if not isinstance(value, _JSONObject):
        raise ConversationCreatedCursorInvalidError()
    digest, created_at_milliseconds = "", 0
    for name, member in value:
        folded = name.casefold()
        if member is None:
            continue
        if folded == "d":
            digest = _go_string(member)
        elif folded == "t":
            created_at_milliseconds = _go_int64(member)
    return _SeenConversation(digest, created_at_milliseconds)


def _decode_conversation_created_cursor(token: str) -> _ConversationCreatedCursor:
    """Decode the token as Go's encoding/json fills the cursor struct.

    Keys match case-insensitively, the last duplicate wins, unknown keys are ignored, a null leaves a
    scalar unchanged and clears the seen list, and a value of the wrong JSON type rejects the token.
    """
    try:
        document = json.loads(
            token,
            object_pairs_hook=_JSONObject,
            parse_float=_JSONFloat,
            parse_constant=_reject_json_constant,
        )
    except (ValueError, RecursionError):
        raise ConversationCreatedCursorInvalidError() from None
    position = _ConversationCreatedCursor()
    if document is None:
        return position
    if not isinstance(document, _JSONObject):
        raise ConversationCreatedCursorInvalidError()
    for name, value in document:
        folded = name.casefold()
        if folded == "seen":
            if value is None:
                position.seen = []
            elif isinstance(value, list) and not isinstance(value, _JSONObject):
                position.seen = [_decode_seen_conversation(entry) for entry in value]
            else:
                raise ConversationCreatedCursorInvalidError()
        elif value is None:
            continue
        elif folded == "brand":
            position.brand = _go_string(value)
        elif folded == "channel":
            position.channel = _go_string(value)
        elif folded == "watermark":
            position.watermark_milliseconds = _go_int64(value)
        elif folded == "floor":
            position.floor_milliseconds = _go_int64(value)
    return position


def advance_conversation_created_cursor(
    position: _ConversationCreatedCursor, walk: _ConversationWalk
) -> _ConversationCreatedCursor:
    """Move the watermark to the newest walked conversation and keep every one at or above the floor seen.

    A walk that stopped at the page bound, such as a seed, raises the floor just past its oldest
    conversation, and trimming the seen set raises it just past the newest entry dropped. Both raises
    exclude a whole creation millisecond, so a conversation that shares it and was not read, or whose seen
    entry was dropped, is never delivered later as new.
    """
    next_position = _ConversationCreatedCursor(
        position.brand, position.channel, position.watermark_milliseconds
    )
    oldest_walked_milliseconds = 0
    for index, conversation in enumerate(walk.conversations):
        created_at_milliseconds = unix_milliseconds(conversation.created_at)
        next_position.watermark_milliseconds = max(
            next_position.watermark_milliseconds, created_at_milliseconds
        )
        if index == 0 or created_at_milliseconds < oldest_walked_milliseconds:
            oldest_walked_milliseconds = created_at_milliseconds
    next_position.floor_milliseconds = max(
        position.floor_milliseconds, next_position.watermark_milliseconds - _LATE_INDEX_WINDOW_MILLISECONDS
    )
    if not walk.reaches_floor and walk.conversations:
        next_position.floor_milliseconds = max(
            next_position.floor_milliseconds, oldest_walked_milliseconds + 1
        )
    newest_by_digest: dict[str, int] = {}
    for entry in position.seen:
        newest_by_digest[entry.digest] = max(
            newest_by_digest.get(entry.digest, 0), entry.created_at_milliseconds
        )
    for conversation in walk.conversations:
        digest = conversation_digest(conversation.id)
        newest_by_digest[digest] = max(
            newest_by_digest.get(digest, 0), unix_milliseconds(conversation.created_at)
        )
    seen = [
        _SeenConversation(digest, created_at_milliseconds)
        for digest, created_at_milliseconds in newest_by_digest.items()
        if created_at_milliseconds >= next_position.floor_milliseconds
    ]
    seen.sort(key=lambda entry: (-entry.created_at_milliseconds, entry.digest.encode()))
    if len(seen) > _MAXIMUM_SEEN_CONVERSATIONS:
        next_position.floor_milliseconds = max(
            next_position.floor_milliseconds,
            seen[_MAXIMUM_SEEN_CONVERSATIONS].created_at_milliseconds + 1,
        )
        seen = [
            entry
            for entry in seen[:_MAXIMUM_SEEN_CONVERSATIONS]
            if entry.created_at_milliseconds >= next_position.floor_milliseconds
        ]
    next_position.seen = seen
    return next_position


class ConversationCreatedFetcher:
    """Poll one brand's, or one channel's, GET /conversations for the poll Trigger kit.

    It implements `ChangeFetcher[TriggerEvent[ConversationCreatedEvent]]` at parity with the Go connector.
    Re:amaze lists conversations by creation time with no change token, so the cursor holds the newest
    creation time, a floor ten minutes behind it below which nothing is delivered, and digests of the
    conversations already seen at or above the floor. A poller owns one fetcher and calls it from one task
    at a time.
    """

    def __init__(
        self,
        client: Client,
        connection: ConnectionRef,
        binding_name: str,
        configuration: ConversationCreatedTriggerConfiguration,
        logger: TriggerLogger | None = None,
    ) -> None:
        """Bind the fetcher to one connection's client and one binding's channel.

        logger receives the WARN record of a backlog larger than one poll reads; None uses standard
        logging.
        """
        if client is None or connection is None or configuration is None:
            raise ValueError(
                "Re:amaze conversationCreated client, connection, and configuration are required"
            )
        self._client = client
        self._call = new_trigger_credential_call(connection, CONVERSATION_CREATED_TRIGGER_DEFINITION.trigger)
        self._channel = configuration.channel
        self._binding_name = binding_name
        self._logger: TriggerLogger = logger if logger is not None else StandardLoggingTriggerLogger()

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Read only the newest page, so existing conversations become the baseline and never arrive."""
        walk = await self._walk_newest_conversations(context, 0, is_seed=True)
        empty = _ConversationCreatedCursor(brand=self._client.brand, channel=self._channel)
        return _encode_conversation_created_cursor(advance_conversation_created_cursor(empty, walk))

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[ConversationCreatedEvent]]:
        """Deliver every listed conversation at or above the floor that the cursor has not seen, oldest first.

        An undecodable cursor, or one for another brand or channel, raises `mark_cursor_invalid`, so the
        kit records a new baseline; a 429 raises `mark_rate_limited` with Retry-After, or one minute.
        """
        try:
            position = _decode_conversation_created_cursor(cursor.token)
        except ConversationCreatedCursorInvalidError as error:
            raise mark_cursor_invalid(error) from None
        if not position.brand:
            raise mark_cursor_invalid(ConversationCreatedCursorInvalidError())
        if position.brand != self._client.brand or position.channel != self._channel:
            raise mark_cursor_invalid(
                ValueError("Re:amaze conversationCreated cursor belongs to another brand or channel")
            )
        walk = await self._walk_newest_conversations(context, position.floor_milliseconds, is_seed=False)
        if not walk.reaches_floor:
            self._warn_backlog_skipped(walk.pages_read)
        seen = {entry.digest for entry in position.seen}
        fresh = [
            conversation
            for conversation in walk.conversations
            if unix_milliseconds(conversation.created_at) >= position.floor_milliseconds
            and conversation_digest(conversation.id) not in seen
        ]
        fresh.sort(key=lambda conversation: (conversation.created_at, conversation.id.encode()))
        changes = tuple(
            _new_conversation_created_trigger_event(self._client.brand, self._channel, conversation)
            for conversation in fresh
        )
        next_cursor = _encode_conversation_created_cursor(advance_conversation_created_cursor(position, walk))
        return ChangePage(changes, next_cursor)

    def _warn_backlog_skipped(self, pages_read: int) -> None:
        if self._logger.is_enabled_for(TriggerLogLevel.WARN):
            self._logger.log(
                TriggerLogLevel.WARN,
                "poll backlog exceeds page bound; older new conversations skipped",
                {
                    "connector": CONNECTOR_ID,
                    "trigger": _TRIGGER_NAME,
                    "binding": self._binding_name,
                    "pages": pages_read,
                },
            )

    async def _walk_newest_conversations(
        self, context: TriggerContext, floor_milliseconds: int, *, is_seed: bool
    ) -> _ConversationWalk:
        """Read pages from the newest end of the listing until one holds a conversation below the floor.

        Every conversation at the floor, ties across a page edge included, is read; the walk also stops at
        the end of the listing or after the page bound, and a seed reads only the newest page. Re:amaze
        documents its default order as creation time without a direction, so the walk detects it from the
        first page and starts from the last page when it ascends.
        """
        first = await self._list_conversation_page(context, 1)
        walk = _ConversationWalk(pages_read=1)
        listed = first.conversations
        is_ascending = len(listed) > 1 and listed[0].created_at < listed[-1].created_at
        page_number, step, current = 1, 1, first
        if is_ascending and first.page_count > 1:
            if first.page_count > _MAXIMUM_SEARCH_PAGE:
                raise RuntimeError(
                    f"Re:amaze lists {first.page_count} conversation pages oldest first, more than the "
                    f"{_MAXIMUM_SEARCH_PAGE} a poll can reach"
                )
            page_number, step = first.page_count, -1
            current = await self._list_conversation_page(context, page_number)
            walk.pages_read += 1
        listed_ids: set[str] = set()
        while True:
            is_below_floor = False
            for conversation in current.conversations:
                if unix_milliseconds(conversation.created_at) < floor_milliseconds:
                    is_below_floor = True
                if conversation.id not in listed_ids:
                    listed_ids.add(conversation.id)
                    walk.conversations.append(conversation)
            next_page_number = page_number + step
            is_last_page = (
                not current.conversations
                or next_page_number < 1
                or next_page_number > min(current.page_count, _MAXIMUM_SEARCH_PAGE)
            )
            if is_below_floor or is_last_page:
                walk.reaches_floor = True
                return walk
            if is_seed or walk.pages_read >= MAXIMUM_CONVERSATION_CREATED_PAGES_PER_POLL:
                return walk
            page_number = next_page_number
            current = await self._list_conversation_page(context, page_number)
            walk.pages_read += 1

    async def _list_conversation_page(
        self, context: TriggerContext, page_number: int
    ) -> SearchConversationsOutput:
        """Send one GET /conversations request for every view, so archived and spam ones list too."""
        await wait_for_poll_request(context)
        try:
            credentials = await self._client.resolve_credentials(self._call)
        except (CredentialsUnavailableError, CredentialsUnusableError):
            raise RuntimeError(
                "Re:amaze conversationCreated poll: connection credentials are unavailable"
            ) from None
        try:
            query = _search_query(
                SearchConversationsInput(filter="all", channel=self._channel, page=page_number)
            )
        except ValueError as error:
            raise ValueError(f"reamaze conversationCreated configuration: {error}") from None
        exchange = await self._client.exchange(
            self._call, credentials, _OPERATION, "GET", "/conversations", query=query
        )
        message = (
            exchange.failure.message if exchange.failure is not None else "Re:amaze returned no response"
        )
        if exchange.outcome == "rate_limited":
            # Re:amaze limits requests per minute per API token; the limit is shared with operation Steps.
            raise mark_rate_limited(
                RuntimeError("Re:amaze conversationCreated poll: " + message),
                timedelta(seconds=exchange.retry_after_seconds),
            )
        if exchange.outcome != "succeeded":
            raise RuntimeError("Re:amaze conversationCreated poll: " + message)
        try:
            return decode_conversation_page(exchange.body, page_number, is_first_message_included=True)
        except (ValueError, TypeError) as error:
            raise ValueError(
                f"Re:amaze conversationCreated poll: invalid conversation page: {error}"
            ) from None
