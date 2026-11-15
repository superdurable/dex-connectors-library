# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The meetingChanged fetcher: full listings of Zoom's List meetings diffed against a keyed snapshot."""

import json
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from typing import Final

from dex_connectors.sdk import (
    ConnectionRef,
    ReauthorizationRequiredError,
    TriggerContext,
    TriggerEvent,
    TriggerRef,
)
from dex_connectors.sdk.pollingtrigger import (
    DEFAULT_KEYED_SNAPSHOT_ENTRIES,
    ChangePage,
    Cursor,
    KeyedItem,
    KeyedSnapshotDiff,
    KeyedSnapshotOptions,
    apply_keyed_snapshot_changes,
    diff_keyed_snapshot,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import KeyedSnapshot, KeyedSnapshotEntry
from dex_connectors.sdk.providerhttp import is_header_safe_credential, parse_retry_after

from ._meeting_snapshot import (
    ListedMeeting,
    MeetingChangeKind,
    _go_json,
    decode_tracked_meeting,
    delivers_meeting_change,
    new_meeting_changed_trigger_event,
)
from .client import Client, Credentials, classify
from .meeting import decode_meeting_page
from .models import MeetingChangedEvent, MeetingChangedTriggerConfiguration, MeetingPage, MeetingSummary

MEETING_LIST_CURSOR_VERSION: Final = 1
"""Versions the committed cursor token; another value records a new baseline."""
_MAXIMUM_PAGE_SIZE: Final = 300
# The snapshot bound at Zoom's largest page size, plus one page.
MAXIMUM_MEETING_LIST_PAGES: Final = DEFAULT_KEYED_SNAPSHOT_ENTRIES // _MAXIMUM_PAGE_SIZE + 2
MEETING_CHANGES_PER_PAGE: Final = 100
"""Keeps one commit small; further changes follow in the same poll."""
MEETING_SNAPSHOT_OPTIONS: Final = KeyedSnapshotOptions(keep_values=True)
_LIST_MEETINGS_OPERATION: Final = "listMeetings"


class _PageTokenRejectedError(Exception):
    """Zoom answered 400 to a later page, as it does after the 15-minute page token expiry."""


@dataclass(slots=True)
class _MeetingListCursor:
    """The committed token: HostID owns the snapshot, Sequence counts commits that carried a difference."""

    version: int = 0
    host_id: str = ""
    sequence: int = 0

    def encode(self) -> str:
        fields = [f'"version":{self.version}']
        if self.host_id:
            fields.append(f'"hostId":{_go_json(self.host_id)}')
        if self.sequence:
            fields.append(f'"sequence":{self.sequence}')
        return "{" + ",".join(fields) + "}"

    @classmethod
    def decode(cls, token: str) -> "_MeetingListCursor | None":
        try:
            value = json.loads(token)
        except ValueError:
            return None
        if value is None:
            return cls()
        if not isinstance(value, dict):
            return None
        version, host_id, sequence = value.get("version"), value.get("hostId"), value.get("sequence")
        if version is not None and type(version) is not int:
            return None
        if host_id is not None and not isinstance(host_id, str):
            return None
        if sequence is not None and type(sequence) is not int:
            return None
        return cls(version or 0, host_id or "", sequence or 0)


@dataclass(slots=True)
class _MeetingListing:
    """One complete listing, one entry per meeting ID, sorted by key."""

    meetings: list[ListedMeeting] = field(default_factory=list)
    host_ids: set[str] = field(default_factory=set)
    by_key: dict[str, ListedMeeting] = field(default_factory=dict)

    def sole_host_id(self) -> str:
        """The host when every listed meeting names the same one, and "" otherwise."""
        return next(iter(self.host_ids)) if len(self.host_ids) == 1 else ""


@dataclass(frozen=True, slots=True)
class _MeetingSnapshotChange:
    kind: MeetingChangeKind
    key: str
    previous: KeyedSnapshotEntry | None = None
    current: ListedMeeting | None = None


class MeetingListFetcher:
    """Diffs full meeting listings; Zoom has no change feed, so the snapshot is the cursor."""

    def __init__(
        self,
        client: Client,
        connection: ConnectionRef,
        trigger: TriggerRef,
        configuration: MeetingChangedTriggerConfiguration,
    ) -> None:
        self._client = client
        self._configuration = configuration
        self._call = new_trigger_credential_call(connection, trigger)

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """List every scheduled meeting and keep the listing as the baseline without delivering it."""
        listing = await self._list_all_meetings(context)
        try:
            diff = diff_keyed_snapshot(None, listing.meetings, MEETING_SNAPSHOT_OPTIONS)
        except ValueError as error:
            raise type(error)(f"Zoom meeting baseline: {error}") from error
        position = _MeetingListCursor(MEETING_LIST_CURSOR_VERSION, listing.sole_host_id())
        return Cursor(position.encode(), diff.next_snapshot)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[MeetingChangedEvent]]:
        """Return up to 100 snapshot differences; a foreign or undecodable cursor reseeds."""
        position = _MeetingListCursor.decode(cursor.token)
        if position is None or position.version != MEETING_LIST_CURSOR_VERSION or cursor.snapshot is None:
            raise mark_cursor_invalid(RuntimeError("Zoom meeting list cursor is not decodable"))
        listing = await self._list_all_meetings(context)
        if position.host_id and listing.meetings and position.host_id not in listing.host_ids:
            raise mark_cursor_invalid(
                RuntimeError("Zoom meeting list belongs to another Zoom user than the stored snapshot")
            )
        if not position.host_id:
            position.host_id = listing.sole_host_id()
        try:
            diff = diff_keyed_snapshot(cursor.snapshot, listing.meetings, MEETING_SNAPSHOT_OPTIONS)
        except ValueError as error:
            raise type(error)(f"Zoom meeting snapshot: {error}") from error
        changes = _ordered_meeting_changes(diff, listing)
        next_snapshot = diff.next_snapshot
        sequence = position.sequence
        if changes:
            position.sequence += 1
        has_more_pages = False
        if len(changes) > MEETING_CHANGES_PER_PAGE:
            changes, has_more_pages = changes[:MEETING_CHANGES_PER_PAGE], True
            next_snapshot = _apply_meeting_changes(cursor.snapshot, changes)
        observed_at = self._client.now()
        events = [
            _change_trigger_event(change, sequence, observed_at)
            for change in changes
            if delivers_meeting_change(self._configuration, change.kind)
        ]
        return ChangePage(tuple(events), Cursor(position.encode(), next_snapshot), has_more_pages)

    async def _list_all_meetings(self, context: TriggerContext) -> _MeetingListing:
        # Zoom expires page tokens after 15 minutes, so a 400 on a later page restarts the listing once.
        try:
            return await self._do_list_all_meetings(context)
        except _PageTokenRejectedError:
            pass
        try:
            return await self._do_list_all_meetings(context)
        except _PageTokenRejectedError as error:
            raise RuntimeError(str(error)) from None

    async def _do_list_all_meetings(self, context: TriggerContext) -> _MeetingListing:
        listing = _MeetingListing()
        requested_page_tokens: set[str] = set()
        page_token = ""
        for _ in range(MAXIMUM_MEETING_LIST_PAGES):
            listed = await self._list_meeting_page(context, page_token)
            for summary in listed.meetings:
                meeting = ListedMeeting.from_summary(summary)
                key = meeting.snapshot_key()
                # Zoom can list a meeting twice, such as a recurring meeting's occurrences; the earliest
                # listed start wins, then the smaller hash, so any order agrees.
                existing = listing.by_key.get(key)
                if existing is None or meeting.lists_before(existing):
                    listing.by_key[key] = meeting
                if summary.host_id:
                    listing.host_ids.add(summary.host_id)
            if not listed.next_page_token:
                listing.meetings = [listing.by_key[key] for key in sorted(listing.by_key)]
                return listing
            if listed.next_page_token in requested_page_tokens:
                raise RuntimeError("Zoom meeting list repeated a page token")
            requested_page_tokens.add(listed.next_page_token)
            page_token = listed.next_page_token
        raise RuntimeError(f"Zoom meeting list exceeded {MAXIMUM_MEETING_LIST_PAGES} pages")

    async def _list_meeting_page(self, context: TriggerContext, page_token: str) -> MeetingPage:
        await wait_for_poll_request(context)
        credentials = await self._resolve_credentials()
        query = {"type": "scheduled", "page_size": str(_MAXIMUM_PAGE_SIZE)}
        if page_token:
            query["next_page_token"] = page_token
        exchange = await self._client.exchange(self._call, credentials, "GET", "/users/me/meetings", query)
        context.cancellation.raise_if_cancelled()
        if exchange.error:
            raise RuntimeError("Zoom meeting list request could not reach Zoom")
        if page_token and exchange.status == 400:
            raise _PageTokenRejectedError("Zoom rejected the meeting list page token with HTTP 400")
        route, problem, _, _ = classify(self._client, _LIST_MEETINGS_OPERATION, exchange)
        if route != "usable":
            message = problem.message if problem is not None else "Zoom returned an unusable meeting page"
            failure = RuntimeError("Zoom meeting list request: " + message)
            if exchange.status == 429:
                # A daily-limit 429 rejects a Step; a poller waits Retry-After, or backs off without it.
                retry_after = parse_retry_after(exchange.headers.get("Retry-After", ""), self._client.now())
                raise mark_rate_limited(failure, timedelta(seconds=retry_after))
            raise failure
        try:
            return decode_meeting_page(exchange.body)
        except ValueError:
            raise RuntimeError("Zoom returned an unusable meeting page") from None

    async def _resolve_credentials(self) -> Credentials:
        try:
            credentials = await self._client.credentials.resolve(self._call)
        except ReauthorizationRequiredError:
            raise RuntimeError("Zoom connection requires reauthorization; reconnect the connection") from None
        except Exception:
            raise RuntimeError("Zoom Trigger credentials are unavailable") from None
        if not is_header_safe_credential(credentials.access_token.reveal()):
            raise RuntimeError("Zoom Trigger credentials are unavailable")
        return credentials


def _ordered_meeting_changes(
    diff: KeyedSnapshotDiff, listing: _MeetingListing
) -> list[_MeetingSnapshotChange]:
    """Merge the diff's sorted lists into one list in meeting key order."""
    changes = [
        _MeetingSnapshotChange(MeetingChangeKind.SCHEDULED, added.key, current=listing.by_key[added.key])
        for added in diff.added
    ]
    changes += [
        _MeetingSnapshotChange(
            MeetingChangeKind.UPDATED,
            changed.current.key,
            changed.previous,
            listing.by_key[changed.current.key],
        )
        for changed in diff.changed
    ]
    changes += [
        _MeetingSnapshotChange(MeetingChangeKind.REMOVED, removed.key, removed) for removed in diff.removed
    ]
    changes.sort(key=lambda change: change.key.encode("utf-8"))
    return changes


def _apply_meeting_changes(previous: KeyedSnapshot, changes: list[_MeetingSnapshotChange]) -> KeyedSnapshot:
    """Advance the stored snapshot by only the changes this page returns."""
    upserted: list[KeyedItem] = [change.current for change in changes if change.current is not None]
    removed_keys = [change.key for change in changes if change.kind == MeetingChangeKind.REMOVED]
    try:
        return apply_keyed_snapshot_changes(previous, upserted, removed_keys, MEETING_SNAPSHOT_OPTIONS)
    except ValueError as error:
        raise type(error)(f"Zoom meeting snapshot: {error}") from error


def _change_trigger_event(
    change: _MeetingSnapshotChange, sequence: int, observed_at: datetime
) -> TriggerEvent[MeetingChangedEvent]:
    meeting = change.current.summary if change.current is not None else None
    current_hash = change.current.snapshot_hash() if change.current is not None else ""
    previous_meeting = None
    if change.previous is not None:
        previous = decode_tracked_meeting(change.previous)
        if change.kind == MeetingChangeKind.REMOVED:
            meeting = previous
        else:
            previous_meeting = previous
    payload = MeetingChangedEvent(
        change_kind=change.kind.value,
        meeting=meeting if meeting is not None else MeetingSummary(),
        previous_meeting=previous_meeting,
    )
    return new_meeting_changed_trigger_event(
        change.kind, change.key, change.previous, current_hash, sequence, payload, observed_at
    )
