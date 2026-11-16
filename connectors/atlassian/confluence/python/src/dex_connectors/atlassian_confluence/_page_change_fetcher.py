# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The pageChanged fetcher: Confluence CQL search, newest modification first, with a keyed snapshot."""

from dataclasses import dataclass
from typing import Final

from dex_connectors.sdk import ConnectionRef, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    DEFAULT_KEYED_SNAPSHOT_ENTRIES,
    ChangePage,
    Cursor,
    KeyedItem,
    KeyedSnapshotOptions,
    apply_keyed_snapshot_changes,
    mark_cursor_invalid,
    new_trigger_credential_call,
)
from dex_connectors.sdk.projectconfig import KeyedSnapshot, KeyedSnapshotEntry

from ._generated.definitions import PAGE_CHANGED_TRIGGER_DEFINITION
from ._go_time import GoInstant
from ._page_change_cursor import (
    PageChangeCursor,
    PageChangeScan,
    SeenPageVersion,
    decode_page_change_cursor,
    decode_seen_page_version,
    encode_page_change_cursor,
    encode_seen_page_version,
    page_change_event_id,
    seen_page_hash,
)
from ._page_change_search import (
    ChangedPage,
    SearchCursorRejectedError,
    SearchPageResult,
    is_space_key,
    search_page_changes,
    start_trigger_session,
)
from .client import Client
from .models import PageChangedEvent, PageChangedTriggerConfiguration

PAGE_CHANGE_CREATED: Final = "created"
"""A page whose delivered version is 1, its first published version."""
PAGE_CHANGE_UPDATED: Final = "updated"
"""A page whose delivered version is 2 or later, including one created and edited before the next poll."""
PAGE_CHANGE_LOOKBACK_NANOSECONDS: Final = 10 * 60 * 1_000_000_000
"""Each scan re-reads ten minutes of edits, covering clock skew and search index lag."""
MAXIMUM_SEED_SEARCH_PAGES: Final = 100
"""Bounds the baseline scan of the lookback window."""
PAGE_CHANGE_SNAPSHOT_OPTIONS: Final = KeyedSnapshotOptions(
    max_entries=DEFAULT_KEYED_SNAPSHOT_ENTRIES, keep_values=True, evict_least_recently_seen=True
)
"""One entry per recently changed page; eviction can only repeat a page version, never lose one."""
_TIME_ZONE_MARGIN_NANOSECONDS: Final = 12 * 3600 * 1_000_000_000
_SPACE_KEY_MESSAGE: Final = (
    "pageChanged spaceKey must be a Confluence space key such as OPS, without surrounding whitespace"
)

PageChange = TriggerEvent[PageChangedEvent]


@dataclass(frozen=True, slots=True)
class _SeenPageItem:
    """One page version adapted to the kit's keyed snapshot."""

    page_id: str
    seen: SeenPageVersion

    def snapshot_key(self) -> str:
        return self.page_id

    def snapshot_hash(self) -> str:
        return seen_page_hash(self.page_id, self.seen.version_number)

    def snapshot_value(self) -> bytes:
        return encode_seen_page_version(self.seen)


def validate_page_changed_trigger_configuration(configuration: PageChangedTriggerConfiguration) -> None:
    """Reject a space key that is not a Confluence space key with ValueError; blank is valid."""
    if configuration.space_key and not is_space_key(configuration.space_key):
        raise ValueError(_SPACE_KEY_MESSAGE)


def build_page_change_cql(space_key: str, window_start: GoInstant) -> str:
    """Bound the search by date, widened by half a day because CQL compares in the user's time zone."""
    clauses = "type = page"
    if space_key:
        clauses += " AND space = " + _quote_cql(space_key)
    lower_bound = window_start.utc().add_nanoseconds(-_TIME_ZONE_MARGIN_NANOSECONDS).format_cql_minute()
    return clauses + ' AND lastmodified >= "' + lower_bound + '" ORDER BY lastmodified DESC'


def _quote_cql(value: str) -> str:
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


class PageChangeFetcher:
    """Scans Confluence CQL search, newest modification first, for one pageChanged binding."""

    def __init__(
        self, client: Client, connection: ConnectionRef, configuration: PageChangedTriggerConfiguration
    ) -> None:
        self._client = client
        self._call = new_trigger_credential_call(connection, PAGE_CHANGED_TRIGGER_DEFINITION.trigger)
        self._space_key = configuration.space_key

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Record every page modified in the lookback window as seen, so the first scan delivers none."""
        session = await start_trigger_session(self._client, context, self._call)
        started_at = GoInstant.from_datetime(self._client.now())
        window_start = started_at.add_nanoseconds(-PAGE_CHANGE_LOOKBACK_NANOSECONDS)
        cql = build_page_change_cql(self._space_key, window_start)
        seen: list[KeyedItem] = []
        search_cursor = ""
        for _ in range(MAXIMUM_SEED_SEARCH_PAGES):
            result = await search_page_changes(self._client, context, session, cql, search_cursor)
            seen += [
                _seen_item(page) for page in result.pages if not page.last_modified_at.is_before(window_start)
            ]
            if not result.search_cursor or _has_page_modified_before(result, window_start):
                snapshot = apply_keyed_snapshot_changes(None, seen, (), PAGE_CHANGE_SNAPSHOT_OPTIONS)
                position = PageChangeCursor(session.cloud_id, self._space_key, started_at)
                return _encode_cursor(position, snapshot)
            search_cursor = result.search_cursor
        raise RuntimeError(
            f"Confluence pageChanged baseline exceeded {MAXIMUM_SEED_SEARCH_PAGES} search pages"
        )

    async def fetch_changes(self, context: TriggerContext, cursor: Cursor) -> ChangePage[PageChange]:
        """Read one search page of the current scan; a foreign or undecodable cursor reseeds."""
        try:
            position = decode_page_change_cursor(cursor.token)
        except ValueError:
            position = PageChangeCursor()
        if not position.cloud_id or position.scanned_through.is_zero():
            raise mark_cursor_invalid(RuntimeError("Confluence pageChanged cursor is not decodable"))
        if position.space_key != self._space_key:
            raise mark_cursor_invalid(RuntimeError("Confluence pageChanged cursor belongs to another space"))
        session = await start_trigger_session(self._client, context, self._call)
        if position.cloud_id != session.cloud_id:
            raise mark_cursor_invalid(RuntimeError("Confluence pageChanged cursor belongs to another site"))
        window_start = position.scanned_through.add_nanoseconds(-PAGE_CHANGE_LOOKBACK_NANOSECONDS)
        if position.scan is None:
            started_at = GoInstant.from_datetime(self._client.now())
            position.scan = PageChangeScan(started_at, build_page_change_cql(self._space_key, window_start))
        scan = position.scan
        try:
            result = await search_page_changes(self._client, context, session, scan.cql, scan.search_cursor)
        except SearchCursorRejectedError:
            # Restart this scan from its first page; the window is unchanged, so nothing is skipped.
            scan.search_cursor = ""
            return ChangePage((), _encode_cursor(position, cursor.snapshot), True)
        return _collect_page_changes(session.cloud_id, position, cursor.snapshot, result, window_start)


def _collect_page_changes(
    cloud_id: str,
    position: PageChangeCursor,
    snapshot: KeyedSnapshot | None,
    result: SearchPageResult,
    window_start: GoInstant,
) -> ChangePage[PageChange]:
    """Emit each unseen page version in the window, oldest first; the scan ends at the window's start."""
    changes: list[PageChange] = []
    upserted: list[KeyedItem] = []
    stored = {entry.key: entry for entry in snapshot.entries} if snapshot is not None else {}
    for page in result.pages:
        if page.last_modified_at.is_before(window_start) or _is_seen_page_version(stored, page):
            continue
        changes.append(new_page_changed_trigger_event(cloud_id, page))
        upserted.append(_seen_item(page))
    changes.reverse()
    removed_keys: list[str] = []
    has_more_pages = False
    scan = position.scan
    assert scan is not None
    if not result.search_cursor or _has_page_modified_before(result, window_start):
        position.scanned_through, position.scan = scan.started_at, None
        removed_keys = _expired_seen_page_keys(
            snapshot, position.scanned_through.add_nanoseconds(-PAGE_CHANGE_LOOKBACK_NANOSECONDS)
        )
    else:
        scan.search_cursor = result.search_cursor
        has_more_pages = True
    next_snapshot = apply_keyed_snapshot_changes(
        snapshot, upserted, removed_keys, PAGE_CHANGE_SNAPSHOT_OPTIONS
    )
    return ChangePage(tuple(changes), _encode_cursor(position, next_snapshot), has_more_pages)


def new_page_changed_trigger_event(cloud_id: str, page: ChangedPage) -> PageChange:
    """Convert one search result; its ID is a pure function of site, page, and version."""
    summary = page.summary
    kind = PAGE_CHANGE_CREATED if summary.version_number == 1 else PAGE_CHANGE_UPDATED
    return TriggerEvent(
        page_change_event_id(cloud_id, summary.id, summary.version_number),
        page.last_modified_at.utc().to_datetime(),
        PageChangedEvent(cloud_id=cloud_id, change_kind=kind, page=summary),
    )


def _encode_cursor(position: PageChangeCursor, snapshot: KeyedSnapshot | None) -> Cursor:
    return Cursor(encode_page_change_cursor(position), snapshot if snapshot is not None else KeyedSnapshot())


def _seen_item(page: ChangedPage) -> _SeenPageItem:
    return _SeenPageItem(
        page.summary.id, SeenPageVersion(page.summary.version_number, page.last_modified_at.utc())
    )


def _has_page_modified_before(result: SearchPageResult, window_start: GoInstant) -> bool:
    return any(page.last_modified_at.is_before(window_start) for page in result.pages)


def _is_seen_page_version(stored: dict[str, KeyedSnapshotEntry], page: ChangedPage) -> bool:
    """Also skip an older version a lagging search index still returns."""
    entry = stored.get(page.summary.id)
    if entry is None:
        return False
    try:
        seen = decode_seen_page_version(entry.value)
    except ValueError:
        return False  # An unreadable entry counts as unseen: a repeat, never a loss.
    return seen.version_number >= page.summary.version_number


def _expired_seen_page_keys(snapshot: KeyedSnapshot | None, next_window_start: GoInstant) -> list[str]:
    """List entries no later scan can return again unless the page changes."""
    if snapshot is None:
        return []
    expired = []
    for entry in snapshot.entries:
        try:
            if decode_seen_page_version(entry.value).last_modified_at.is_before(next_window_start):
                expired.append(entry.key)
        except ValueError:
            expired.append(entry.key)
    return expired
