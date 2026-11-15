# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The conversationReceived fetcher: a baseline walk, then the first page plus a sweep on every poll."""

from collections.abc import Callable, Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Final

from dex_connectors.sdk import (
    TriggerBindingRef,
    TriggerContext,
    TriggerEvent,
    TriggerLogger,
    TriggerLogLevel,
    bind_trigger_log_attributes,
)
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    KeyedSnapshotOptions,
    apply_keyed_snapshot_changes,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import MAXIMUM_KEYED_SNAPSHOT_ENTRIES, KeyedSnapshot

from ._conversation_received_cursor import (
    CONVERSATION_RECEIVED_CURSOR_VERSION,
    GO_SPACE,
    ConversationReceivedCursorToken,
    ConversationSnapshotItem,
    conversation_received_event_id,
    decode_conversation_received_cursor_token,
)
from .client import MAXIMUM_PAGE_SIZE, Client, decode_conversation, list_page, page_query, path_escape
from .models import Conversation, ConversationReceivedEvent, ConversationReceivedTriggerConfiguration

CONVERSATION_RECEIVED_OPERATION: Final = "conversationReceived"
MAX_TRACKED_CONVERSATIONS: Final = 50_000
"""Bounds the conversation IDs one binding remembers; a larger inbox pauses its binding as a bound fault."""
SEED_PAGES_PER_FETCH: Final = 10
"""Pages one baseline step reads; the baseline delivers nothing."""
CONVERSATION_RECEIVED_MAX_BACKOFF: Final = timedelta(minutes=30)
"""Caps the wait after failed polls and between bound-fault retries."""
CONVERSATION_SNAPSHOT_OPTIONS: Final = KeyedSnapshotOptions(max_entries=MAX_TRACKED_CONVERSATIONS)
_MERGE_OPTIONS: Final = KeyedSnapshotOptions(max_entries=MAXIMUM_KEYED_SNAPSHOT_ENTRIES)
_REJECTED_PAGE_TOKEN_STATUSES: Final = frozenset({400, 404, 410, 422})


class PollRequestError(Exception):
    """A definite Hiver rejection of one poll request, kept with its HTTP status."""

    def __init__(self, status: int, message: str) -> None:
        super().__init__("Hiver conversationReceived poll: " + message)
        self.status = status


@dataclass(frozen=True, slots=True)
class _ConversationPage:
    conversations: list[Conversation] = field(default_factory=list)
    next_page_token: str = ""


def trim_go_space(value: str) -> str:
    """Trim as Go's strings.TrimSpace does."""
    return value.strip(GO_SPACE)


def _utc_now() -> datetime:
    return datetime.now(UTC)


class ConversationReceivedFetcher:
    """Reads one shared inbox's conversation list; Hiver documents no timestamp, filter, or order for it."""

    def __init__(
        self,
        client: Client,
        binding: TriggerBindingRef,
        configuration: ConversationReceivedTriggerConfiguration,
        logger: TriggerLogger | None = None,
        now: Callable[[], datetime] = _utc_now,
    ) -> None:
        self._client = client
        self._call = new_trigger_credential_call(binding.connection, binding.trigger)
        self._inbox_id = trim_go_space(configuration.inbox_id)
        self._now = now
        self._logger = bind_trigger_log_attributes(
            logger,
            {
                "connector": "hiver",
                "connection": binding.connection.name,
                "trigger": binding.trigger.trigger_name,
                "binding": binding.name,
            },
        )

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Start the baseline walk without a request; fetch_changes records existing conversations."""
        token = ConversationReceivedCursorToken(self._inbox_id, version=CONVERSATION_RECEIVED_CURSOR_VERSION)
        return Cursor(token.encode(), KeyedSnapshot())

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[ConversationReceivedEvent]]:
        """Continue the baseline walk, then read the first page plus sweep pages, delivering new IDs."""
        try:
            token = decode_conversation_received_cursor_token(cursor.token, self._inbox_id)
        except ValueError as error:
            raise mark_cursor_invalid(error) from None
        if not token.is_baseline_complete:
            return await self._continue_baseline(context, token, cursor.snapshot)
        return await self._poll_for_new_conversations(context, token, cursor.snapshot)

    async def _continue_baseline(
        self, context: TriggerContext, token: ConversationReceivedCursorToken, snapshot: KeyedSnapshot | None
    ) -> ChangePage[TriggerEvent[ConversationReceivedEvent]]:
        listed: list[Conversation] = []
        page_token = token.sweep_page_token
        for _ in range(SEED_PAGES_PER_FETCH):
            try:
                read = await self._read_page(context, page_token)
            except PollRequestError as error:
                if not page_token or error.status not in _REJECTED_PAGE_TOKEN_STATUSES:
                    raise
                self._log_walk_restart("baseline")
                page_token = ""
                break
            listed.extend(read.conversations)
            page_token = read.next_page_token
            if not page_token:
                token.is_baseline_complete = True
                break
        next_snapshot = _merge_snapshot(snapshot, listed, ())
        token.sweep_page_token = page_token
        return ChangePage((), Cursor(token.encode(), next_snapshot), not token.is_baseline_complete)

    async def _poll_for_new_conversations(
        self, context: TriggerContext, token: ConversationReceivedCursorToken, snapshot: KeyedSnapshot | None
    ) -> ChangePage[TriggerEvent[ConversationReceivedEvent]]:
        is_sweep_start = not token.sweep_page_token
        if is_sweep_start:
            token.sweep_start_seen = _highest_seen(snapshot) + 1
        head = await self._read_page(context, "")
        listed = list(head.conversations)
        sweep_page_token = head.next_page_token if is_sweep_start else token.sweep_page_token
        is_sweep_complete, is_sweep_restarted = not sweep_page_token, False
        pages = 1
        while not is_sweep_complete and pages < self._client.poll_pages_per_poll:
            pages += 1
            try:
                read = await self._read_page(context, sweep_page_token)
            except PollRequestError as error:
                if error.status not in _REJECTED_PAGE_TOKEN_STATUSES:
                    raise
                self._log_walk_restart("sweep")
                sweep_page_token, is_sweep_restarted = "", True
                break
            listed.extend(read.conversations)
            sweep_page_token = read.next_page_token
            is_sweep_complete = not sweep_page_token
        events = self._new_conversation_events(snapshot, listed)
        removed_keys: list[str] = []
        if is_sweep_complete and not is_sweep_restarted:
            removed_keys = _unlisted_since_sweep_start(snapshot, token.previous_sweep_start_seen, listed)
            token.previous_sweep_start_seen = token.sweep_start_seen
        next_snapshot = _merge_snapshot(snapshot, listed, removed_keys)
        token.sweep_page_token = sweep_page_token
        return ChangePage(tuple(events), Cursor(token.encode(), next_snapshot))

    def _new_conversation_events(
        self, snapshot: KeyedSnapshot | None, listed: Sequence[Conversation]
    ) -> list[TriggerEvent[ConversationReceivedEvent]]:
        """Return one event per listed conversation ID the snapshot lacks, in list order."""
        tracked = {entry.key for entry in snapshot.entries} if snapshot is not None else set()
        occurred_at = self._now().astimezone(UTC)
        events: list[TriggerEvent[ConversationReceivedEvent]] = []
        for conversation in listed:
            if conversation.id in tracked:
                continue
            tracked.add(conversation.id)
            events.append(
                TriggerEvent(
                    conversation_received_event_id(self._inbox_id, conversation.id),
                    occurred_at,
                    ConversationReceivedEvent(inbox_id=self._inbox_id, conversation=conversation),
                )
            )
        return events

    async def _read_page(self, context: TriggerContext, page_token: str) -> _ConversationPage:
        """Wait for the runner's limiter and the connection's request slot, then read one page of 100."""
        await wait_for_poll_request(context)
        credential = await self._client.token(self._call)
        if credential is None:
            raise RuntimeError("Hiver conversationReceived poll: connection credentials are unavailable")
        exchange = await self._client.exchange_poll_request(
            self._call,
            credential,
            CONVERSATION_RECEIVED_OPERATION,
            "/inboxes/" + path_escape(self._inbox_id) + "/conversations",
            page_query(page_token, MAXIMUM_PAGE_SIZE),
        )
        context.cancellation.raise_if_cancelled()
        message = exchange.failure.message if exchange.failure is not None else "the request failed"
        if exchange.outcome == "success":
            return self._decode_page(exchange.body)
        if exchange.outcome == "rateLimited":
            # Without Retry-After the runner backs off exponentially; the client pacer also holds requests.
            raise mark_rate_limited(
                RuntimeError("Hiver conversationReceived poll: " + message),
                timedelta(seconds=exchange.retry_after),
            )
        if exchange.outcome in ("notFound", "rejected"):
            raise PollRequestError(exchange.status, message)
        raise RuntimeError("Hiver conversationReceived poll: " + message)

    def _decode_page(self, body: bytes) -> _ConversationPage:
        """Validate one list page; a page with more conversations than requested is invalid."""
        try:
            results, next_page_token = list_page(body)
        except ValueError as error:
            raise RuntimeError(
                f"Hiver conversationReceived poll: invalid conversation page: {error}"
            ) from None
        if len(results) > MAXIMUM_PAGE_SIZE:
            raise RuntimeError(
                "Hiver conversationReceived poll: the page lists more conversations than requested"
            )
        page = _ConversationPage(next_page_token=next_page_token)
        for index, raw in enumerate(results):
            try:
                if not isinstance(raw, dict):
                    raise ValueError("conversation is not an object")
                page.conversations.append(decode_conversation(raw, self._inbox_id).conversation)
            except (ValueError, TypeError) as error:
                raise RuntimeError(
                    f"Hiver conversationReceived poll: invalid conversation at index {index}: {error}"
                ) from None
        return page

    def _log_walk_restart(self, walk: str) -> None:
        self._logger.log(
            TriggerLogLevel.WARN,
            "hiver page token rejected; restarting walk from the first page",
            {"walk": walk},
        )


def _merge_snapshot(
    snapshot: KeyedSnapshot | None, listed: Sequence[Conversation], removed_keys: Sequence[str]
) -> KeyedSnapshot:
    """Refresh every listed ID as seen by this commit and drop removed_keys.

    The poll kit, not this call, enforces MAX_TRACKED_CONVERSATIONS, so an oversized inbox pauses.
    """
    items: list[ConversationSnapshotItem] = []
    seen_ids: set[str] = set()
    for conversation in listed:
        if conversation.id in seen_ids:
            continue
        seen_ids.add(conversation.id)
        items.append(ConversationSnapshotItem(conversation.id))
    return apply_keyed_snapshot_changes(snapshot, items, removed_keys, _MERGE_OPTIONS)


def _highest_seen(snapshot: KeyedSnapshot | None) -> int:
    """Return the latest commit that refreshed an entry; zero for an empty snapshot."""
    if snapshot is None:
        return 0
    return max((entry.seen for entry in snapshot.entries), default=0)


def _unlisted_since_sweep_start(
    snapshot: KeyedSnapshot | None, sweep_start_seen: int, listed: Sequence[Conversation]
) -> list[str]:
    """Return tracked IDs that no page listed since the commit counter sweep_start_seen."""
    if snapshot is None:
        return []
    listed_now = {conversation.id for conversation in listed}
    return [
        entry.key
        for entry in snapshot.entries
        if entry.key not in listed_now and entry.seen < sweep_start_seen
    ]
