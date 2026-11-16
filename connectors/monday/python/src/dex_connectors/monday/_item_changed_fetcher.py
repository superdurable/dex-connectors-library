# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The itemChanged fetcher: one board's items newest first by monday.com's last-updated time."""

import json
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk import ConnectionRef, FailureKind, TriggerContext, TriggerEvent, TriggerRef
from dex_connectors.sdk.pollingtrigger import (
    DEFAULT_KEYED_SNAPSHOT_ENTRIES,
    ChangePage,
    Cursor,
    KeyedSnapshotOptions,
    apply_keyed_snapshot_changes,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import KeyedSnapshot

from ._item_snapshot import (
    GO_ZERO_TIME_NANOSECONDS,
    NANOSECONDS_PER_SECOND,
    ItemChangeKind,
    PolledItem,
    decode_polled_item,
    format_go_time,
    go_json_string,
    new_item_changed_trigger_event,
    parse_cursor_time,
)
from .client import Client, Exchange
from .models import ItemChangedEvent, ItemChangedTriggerConfiguration
from .operations import _LIST_DOCUMENT, _NEXT_DOCUMENT

ITEM_CHANGED_OPERATION: Final = "itemChanged"
ITEM_POLL_PAGE_SIZE: Final = 100
"""The items_page limit of every poll request, the connector's listItems bound."""
MAXIMUM_ITEM_SCAN_PAGES: Final = 100
"""Bounds one poll's newest-first scan at 10,000 items."""
ITEM_CHANGE_LOOKBACK: Final = timedelta(minutes=5)
"""Rescans items updated shortly before the cursor, so a late or same-second update still arrives."""
MAXIMUM_ITEM_CHANGES_PER_PAGE: Final = 100
MAXIMUM_ITEM_CHANGE_BATCH_BYTES: Final = 4 << 20
"""With the count bound, keeps one recorded batch well below the durable inbox's 1000 events and 8 MiB."""
ITEM_CHANGED_SNAPSHOT_OPTIONS: Final = KeyedSnapshotOptions(
    max_entries=DEFAULT_KEYED_SNAPSHOT_ENTRIES, keep_values=True, evict_least_recently_seen=True
)
_LOOKBACK_NANOSECONDS: Final = int(ITEM_CHANGE_LOOKBACK.total_seconds()) * NANOSECONDS_PER_SECOND
_MAXIMUM_CURSOR_BYTES: Final = 4096
_LAST_UPDATED_ORDER: Final = {"order_by": [{"column_id": "__last_updated__", "direction": "desc"}]}


class ItemScanExceededError(RuntimeError):
    """More items changed since the last poll than one scan reads."""

    def __init__(self) -> None:
        super().__init__(
            f"more than {MAXIMUM_ITEM_SCAN_PAGES * ITEM_POLL_PAGE_SIZE} items changed since the last poll"
        )


@dataclass(frozen=True, slots=True)
class ItemChange:
    """One polled item whose content differs from the snapshot, with its derived kind."""

    polled: PolledItem
    kind: ItemChangeKind


@dataclass(frozen=True, slots=True)
class _ItemScan:
    items: list[PolledItem]
    newest: int | None
    is_exceeded: bool


@dataclass(frozen=True, slots=True)
class _PendingItemChanges:
    cursor_token: str
    changes: list[ItemChange]


class ItemChangeFetcher:
    """Reads one board's items newest first by monday.com's last-updated time.

    The committed cursor token is `{"boardId":...,"watermark":...}`, the latest delivered last-updated
    time, encoded byte for byte as the Go connector does; a zero watermark means the board was empty at
    the baseline, so every item that appears is delivered.
    """

    def __init__(
        self,
        client: Client,
        connection: ConnectionRef,
        trigger: TriggerRef,
        configuration: ItemChangedTriggerConfiguration,
    ) -> None:
        self._client = client
        self._call = new_trigger_credential_call(connection, trigger)
        self._board_id = configuration.board_id
        # The changes a scan found beyond the delivered batch, for the immediate next fetch.
        self._pending: _PendingItemChanges | None = None

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Record the newest last-updated time and snapshot the items inside the lookback window before it."""
        self._pending = None
        # An oversized baseline window keeps the items read so far; the rest may arrive once as changes.
        scan = await self._scan_items_updated_since(context, None, is_floor_from_newest=True)
        snapshot = apply_keyed_snapshot_changes(None, scan.items, (), ITEM_CHANGED_SNAPSHOT_OPTIONS)
        return Cursor(self._encode_cursor(scan.newest), snapshot)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[ItemChangedEvent]]:
        """Deliver, oldest first, the items whose content changed since cursor.

        An undecodable cursor, a cursor for another board, or more changes than one scan can read raise
        `mark_cursor_invalid`.
        """
        board_id, watermark = self._decode_cursor(cursor.token)
        if board_id != self._board_id:
            raise mark_cursor_invalid(RuntimeError("monday.com itemChanged cursor belongs to another board"))
        changes = await self._changes_after(context, cursor, watermark)
        batch_size = item_change_batch_size(changes)
        batch, rest = changes[:batch_size], changes[batch_size:]
        next_watermark = watermark
        for change in batch:
            updated_at = change.polled.updated_at
            if updated_at is not None and (next_watermark is None or updated_at > next_watermark):
                next_watermark = updated_at
        events = [
            new_item_changed_trigger_event(self._board_id, change.polled, change.kind) for change in batch
        ]
        snapshot = _next_item_snapshot(cursor.snapshot, batch, _floor_of(next_watermark))
        next_cursor = Cursor(self._encode_cursor(next_watermark), snapshot)
        self._pending = None
        if rest:
            self._pending = _PendingItemChanges(next_cursor.token, rest)
        return ChangePage(tuple(events), next_cursor, bool(rest))

    async def _changes_after(
        self, context: TriggerContext, cursor: Cursor, watermark: int | None
    ) -> list[ItemChange]:
        pending = self._pending
        if pending is not None and pending.cursor_token == cursor.token:
            return pending.changes
        self._pending = None
        floor = _floor_of(watermark)
        scan = await self._scan_items_updated_since(context, floor, is_floor_from_newest=False)
        if scan.is_exceeded:
            raise mark_cursor_invalid(ItemScanExceededError())
        previous_hashes = {
            entry.key: entry.hash for entry in (cursor.snapshot.entries if cursor.snapshot else ())
        }
        changes: list[ItemChange] = []
        for polled in scan.items:
            previous_hash = previous_hashes.get(polled.item.id)
            if previous_hash == polled.snapshot_hash():
                continue
            kind = ItemChangeKind.UPDATED
            if previous_hash is None and polled.created_at is not None and polled.created_at >= floor:
                kind = ItemChangeKind.CREATED
            changes.append(ItemChange(polled, kind))
        changes.sort(key=lambda change: (change.polled.updated_at or 0, change.polled.item.id.encode()))
        return changes

    async def _scan_items_updated_since(
        self, context: TriggerContext, floor: int | None, *, is_floor_from_newest: bool
    ) -> _ItemScan:
        """Read pages newest first until an item older than floor or the last page.

        With is_floor_from_newest the floor becomes the newest item's time minus the lookback.
        """
        document = _LIST_DOCUMENT
        variables: dict[str, Any] = {
            "boardIds": [self._board_id],
            "limit": ITEM_POLL_PAGE_SIZE,
            "columnIds": None,
            "queryParams": _LAST_UPDATED_ORDER,
        }
        items: list[PolledItem] = []
        newest: int | None = None
        seen_item_ids: set[str] = set()
        for page_index in range(MAXIMUM_ITEM_SCAN_PAGES):
            listed, next_cursor = await self._read_items_page(context, document, variables, page_index > 0)
            has_reached_floor = False
            for polled in listed:
                updated_at = polled.updated_at
                if updated_at is None or polled.item.id in seen_item_ids:
                    continue
                seen_item_ids.add(polled.item.id)
                if newest is None and is_floor_from_newest:
                    floor = updated_at - _LOOKBACK_NANOSECONDS
                if newest is None or updated_at > newest:
                    newest = updated_at
                if floor is not None and updated_at < floor:
                    has_reached_floor = True
                    continue
                items.append(polled)
            if has_reached_floor or not next_cursor:
                return _ItemScan(items, newest, False)
            document = _NEXT_DOCUMENT
            variables = {"cursor": next_cursor, "limit": ITEM_POLL_PAGE_SIZE, "columnIds": None}
        return _ItemScan(items, newest, True)

    async def _read_items_page(
        self, context: TriggerContext, document: str, variables: dict[str, Any], is_continuation: bool
    ) -> tuple[list[PolledItem], str]:
        await wait_for_poll_request(context)
        exchange = await self._client.exchange(self._call, ITEM_CHANGED_OPERATION, document, variables)
        context.cancellation.raise_if_cancelled()
        if exchange.outcome != "succeeded":
            raise self._poll_error(exchange)
        try:
            page = _decode_items_page(exchange.data, is_continuation)
        except ValueError as error:
            raise RuntimeError(
                f"monday.com itemChanged poll returned an invalid page of items: {error}"
            ) from None
        if page is None:
            raise RuntimeError(
                "monday.com itemChanged poll found no board with this ID that the connection can see"
            )
        return page

    def _poll_error(self, exchange: Exchange) -> Exception:
        """Pause the connection for a rate or daily call limit; other failures back off the binding."""
        message = exchange.problem.message if exchange.problem is not None else "request failed"
        failure = RuntimeError(f"monday.com itemChanged poll: {message}")
        kind = exchange.problem.kind if exchange.problem is not None else None
        if kind == FailureKind.RATE_LIMIT:
            return mark_rate_limited(failure, timedelta(seconds=exchange.retry_after))
        if kind == FailureKind.QUOTA_EXHAUSTED:
            now = self._client.now().astimezone(UTC)
            next_midnight = datetime(now.year, now.month, now.day, tzinfo=UTC) + timedelta(days=1)
            return mark_rate_limited(
                failure, max(timedelta(seconds=exchange.retry_after), next_midnight - now)
            )
        return failure

    def _encode_cursor(self, watermark: int | None) -> str:
        return f'{{"boardId":{go_json_string(self._board_id)},"watermark":"{format_go_time(watermark)}"}}'

    @staticmethod
    def _decode_cursor(token: str) -> tuple[str, int | None]:
        """Decode the token as Go's json.Unmarshal fills itemChangeCursor, keys matched case-insensitively."""
        invalid = mark_cursor_invalid(RuntimeError("monday.com itemChanged cursor is not decodable"))
        try:
            decoded = json.loads(token)
        except ValueError:
            raise invalid  # noqa: B904 - the marked error carries its own cause.
        if decoded is None:
            return "", None
        if not isinstance(decoded, dict):
            raise invalid
        board_id, watermark = "", None
        for key, value in decoded.items():
            if key.casefold() == "boardid":
                if value is not None and not isinstance(value, str):
                    raise invalid
                board_id = value if value is not None else board_id
            elif key.casefold() == "watermark" and value is not None:
                if not isinstance(value, str):
                    raise invalid
                watermark = parse_cursor_time(value)
                if watermark is None:
                    raise invalid
        if watermark == GO_ZERO_TIME_NANOSECONDS:
            watermark = None
        return board_id, watermark


def item_change_batch_size(changes: list[ItemChange]) -> int:
    """Count the oldest changes that fit one batch; the first change always fits."""
    encoded_bytes = 0
    for index, change in enumerate(changes):
        encoded_bytes += len(change.polled.encoded)
        if index == MAXIMUM_ITEM_CHANGES_PER_PAGE or (
            index > 0 and encoded_bytes > MAXIMUM_ITEM_CHANGE_BATCH_BYTES
        ):
            return index
    return len(changes)


def _floor_of(watermark: int | None) -> int:
    # Go's zero time minus the lookback is still before every real item.
    return (watermark if watermark is not None else GO_ZERO_TIME_NANOSECONDS) - _LOOKBACK_NANOSECONDS


def _next_item_snapshot(
    previous: KeyedSnapshot | None, delivered: list[ItemChange], floor: int
) -> KeyedSnapshot:
    """Add the delivered items and drop entries updated before floor, which no later scan reads."""
    snapshot = apply_keyed_snapshot_changes(
        previous, [change.polled for change in delivered], (), ITEM_CHANGED_SNAPSHOT_OPTIONS
    )
    expired_keys = [entry.key for entry in snapshot.entries if _is_entry_expired(entry.value, floor)]
    if not expired_keys:
        return snapshot
    return apply_keyed_snapshot_changes(snapshot, (), expired_keys, ITEM_CHANGED_SNAPSHOT_OPTIONS)


def _is_entry_expired(value: bytes | None, floor: int) -> bool:
    try:
        decoded = json.loads(value or b"")
    except ValueError:
        return True
    updated_at: int | None = None
    if isinstance(decoded, dict):
        for key, item in decoded.items():
            if key.casefold() == "updatedat":
                if item is not None and not isinstance(item, str):
                    return True
                if isinstance(item, str):
                    updated_at = parse_cursor_time(item)
                    if updated_at is None:
                        return True
    elif decoded is not None:
        return True
    return (updated_at if updated_at is not None else GO_ZERO_TIME_NANOSECONDS) < floor


def _decode_items_page(data: Any, is_continuation: bool) -> tuple[list[PolledItem], str] | None:
    """Read boards[0].items_page, or next_items_page; None when the board is not visible."""
    if is_continuation:
        resource = data.get("next_items_page") if isinstance(data, dict) else None
        if not isinstance(resource, dict):
            raise ValueError("next_items_page is missing")
    else:
        if data is not None and not isinstance(data, dict):
            raise ValueError("boards is not a list")
        boards = data.get("boards") if isinstance(data, dict) else None
        if boards is not None and not isinstance(boards, list):
            raise ValueError("boards is not a list")
        if not boards or boards[0] is None:
            return None
        if (
            len(boards) != 1
            or not isinstance(boards[0], dict)
            or not isinstance(boards[0].get("items_page"), dict)
        ):
            raise ValueError("expected one board with an items_page")
        resource = boards[0]["items_page"]
    next_cursor = ""
    cursor = resource.get("cursor")
    if cursor is not None:
        if (
            not isinstance(cursor, str)
            or not cursor
            or len(cursor.encode()) > _MAXIMUM_CURSOR_BYTES
            or not all(33 <= ord(character) <= 126 for character in cursor)
        ):
            raise ValueError("cursor is not a printable token")
        next_cursor = cursor
    items = resource.get("items")
    if items is None:
        items = []
    if not isinstance(items, list):
        raise ValueError("items is not a list")
    if len(items) > ITEM_POLL_PAGE_SIZE:
        raise ValueError(f"page holds more than {ITEM_POLL_PAGE_SIZE} items")
    return [decode_polled_item(item) for item in items], next_cursor
