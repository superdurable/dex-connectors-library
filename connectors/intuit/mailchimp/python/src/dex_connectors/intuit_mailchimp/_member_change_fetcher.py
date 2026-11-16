# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The memberChanged fetcher: one audience's contacts listed by last change time for the poll kit."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk import ConnectionRef, TriggerContext, TriggerEvent, TriggerRef
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    KeyedItem,
    KeyedSnapshotOptions,
    apply_keyed_snapshot_changes,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import KeyedSnapshot

from ._member_snapshot import (
    MemberSnapshotItem,
    accepts_member_status,
    new_member_changed_trigger_event,
)
from .client import Client
from .models import Member, MemberChangedEvent, MemberChangedTriggerConfiguration
from .operations import _MEMBER_LIST_FIELDS, _decode_member, _decode_object

MEMBER_POLL_PAGE_SIZE: Final = 250
"""Stays below the kit's 500 changes per page and Mailchimp's 1000-contact count."""
MEMBER_POLL_LOOKBACK: Final = timedelta(minutes=5)
"""Rereads recent changes, because a change can become listable after a later one."""
MAXIMUM_MEMBER_POLL_OFFSET: Final = 1_000_000
"""Bounds tie paging of contacts that share one change second."""
MEMBER_SNAPSHOT_OPTIONS: Final = KeyedSnapshotOptions(evict_least_recently_seen=True)
"""Keeps the most recently changed contacts; older ones are outside the lookback."""
_OPERATION: Final = "memberChanged"
_SECOND: Final = timedelta(seconds=1)
_CURSOR_TIME: Final = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})\Z"
)
_ChangeEvent = TriggerEvent[MemberChangedEvent]


@dataclass(slots=True)
class _MemberPollCursor:
    """The committed token; times are RFC 3339 seconds in UTC, as Go's memberPollCursor encodes them."""

    audience_id: str = ""
    baseline: str = ""
    """The newest change second at seed time; contacts changed before it are never delivered."""
    watermark: str = ""
    """The newest change time any pass has read."""
    pass_since: str = ""
    pass_offset: int = 0
    """pass_since and pass_offset continue a pass that spans pages; both are empty between passes."""

    def encode(self) -> str:
        fields = [f'"audienceId":{json.dumps(self.audience_id)}']
        for name, value in (
            ("baseline", self.baseline),
            ("watermark", self.watermark),
            ("passSince", self.pass_since),
        ):
            if value:
                fields.append(f'"{name}":"{value}"')
        if self.pass_offset:
            fields.append(f'"passOffset":{self.pass_offset}')
        return "{" + ",".join(fields) + "}"

    def pass_start(self) -> datetime | None:
        """The pending pass's bound, or the lookback before the watermark, never before baseline - 1s."""
        if self.pass_since or self.pass_offset:
            return _parse_cursor_time(self.pass_since)
        watermark = _parse_cursor_time(self.watermark)
        if watermark is None:
            return None
        start = watermark - MEMBER_POLL_LOOKBACK
        baseline = _parse_cursor_time(self.baseline)
        # One second before the baseline lists the baseline second whether since_last_changed is exclusive.
        if baseline is not None and start < baseline - _SECOND:
            start = baseline - _SECOND
        return start


@dataclass(frozen=True, slots=True)
class _MemberPage:
    members: tuple[Member, ...]
    total_items: int


class MemberChangeFetcher:
    """Lists one audience's contacts by last change time; the snapshot drops versions already delivered."""

    def __init__(
        self,
        client: Client,
        connection: ConnectionRef,
        trigger: TriggerRef,
        configuration: MemberChangedTriggerConfiguration,
    ) -> None:
        self._client = client
        self._configuration = configuration
        self._call = new_trigger_credential_call(connection, trigger)

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Record the newest change second as the baseline and snapshot the contacts changed in it.

        A contact changed later in that second is still delivered. An empty audience has no baseline. When
        a whole page shares the newest second, the baseline moves one second later and none of that
        second's later changes are delivered.
        """
        page = await self._list_members(
            context,
            {
                "count": str(MEMBER_POLL_PAGE_SIZE),
                "offset": "0",
                "sort_field": "last_changed",
                "sort_dir": "DESC",
                "fields": _MEMBER_LIST_FIELDS,
            },
            0,
        )
        cursor = _MemberPollCursor(self._configuration.audience_id)
        newest: datetime | None = None
        seeded: list[KeyedItem] = []
        for member in page.members:
            if member.last_changed_at is None:
                continue
            if newest is None:
                newest = member.last_changed_at
            if member.last_changed_at != newest:
                break
            seeded.append(MemberSnapshotItem(self._configuration.audience_id, member))
        if newest is None:
            # No snapshot, as Go commits it: the stored cursor document then holds "snapshot":null.
            return Cursor(cursor.encode(), None)
        cursor.baseline = cursor.watermark = _format_cursor_time(newest)
        if len(seeded) == len(page.members) == MEMBER_POLL_PAGE_SIZE:
            # More contacts may share the newest second than one page holds, so none of it is delivered.
            cursor.baseline, seeded = _format_cursor_time(newest + _SECOND), []
        snapshot = apply_keyed_snapshot_changes(None, seeded, [], MEMBER_SNAPSHOT_OPTIONS)
        return Cursor(cursor.encode(), snapshot)

    async def fetch_changes(self, context: TriggerContext, cursor: Cursor) -> ChangePage[_ChangeEvent]:
        """Read one ascending page of changed contacts after the cursor's pass start."""
        try:
            position = self._decode_cursor(cursor.token)
            since = position.pass_start()
        except ValueError as error:
            raise mark_cursor_invalid(error) from None
        query = {
            "count": str(MEMBER_POLL_PAGE_SIZE),
            "offset": str(position.pass_offset),
            "sort_field": "last_changed",
            "sort_dir": "ASC",
            "fields": _MEMBER_LIST_FIELDS,
        }
        if since is not None:
            query["since_last_changed"] = since.strftime("%Y-%m-%dT%H:%M:%S") + "+00:00"
        page = await self._list_members(context, query, position.pass_offset)
        return self._build_change_page(position, since, page, cursor.snapshot)

    def _build_change_page(
        self,
        position: _MemberPollCursor,
        since: datetime | None,
        page: _MemberPage,
        snapshot: KeyedSnapshot | None,
    ) -> ChangePage[_ChangeEvent]:
        audience_id = self._configuration.audience_id
        baseline = _parse_cursor_time(position.baseline)
        watermark = _parse_cursor_time(position.watermark)
        stored = {entry.key: entry.hash for entry in snapshot.entries} if snapshot is not None else {}
        events: list[_ChangeEvent] = []
        upserted: list[KeyedItem] = []
        listed: list[Member] = []
        for member in page.members:
            changed_at = member.last_changed_at
            if changed_at is None:
                # Without a change time a contact cannot be ordered; Mailchimp sets it on every write.
                continue
            listed.append(member)
            if watermark is None or changed_at > watermark:
                watermark = changed_at
            item = MemberSnapshotItem(audience_id, member)
            if baseline is not None and changed_at < baseline:
                continue
            if stored.get(item.snapshot_key()) == item.snapshot_hash():
                continue
            upserted.append(item)
            if accepts_member_status(self._configuration, member.status):
                events.append(new_member_changed_trigger_event(audience_id, member))
        next_snapshot = apply_keyed_snapshot_changes(snapshot, upserted, [], MEMBER_SNAPSHOT_OPTIONS)
        following = _MemberPollCursor(position.audience_id, position.baseline)
        if watermark is not None:
            following.watermark = _format_cursor_time(watermark)
        has_more_pages = (
            len(page.members) == MEMBER_POLL_PAGE_SIZE
            and position.pass_offset + len(page.members) < page.total_items
        )
        if has_more_pages:
            following.pass_since, following.pass_offset = _continue_pass(
                since, position.pass_offset, len(page.members), listed
            )
        return ChangePage(tuple(events), Cursor(following.encode(), next_snapshot), has_more_pages)

    def _decode_cursor(self, token: str) -> _MemberPollCursor:
        try:
            value: Any = json.loads(token)
        except ValueError:
            raise ValueError("Mailchimp memberChanged cursor is not valid JSON") from None
        if value is None:
            value = {}
        if not isinstance(value, dict):
            raise ValueError("Mailchimp memberChanged cursor is not valid JSON")
        texts: dict[str, str] = {}
        for name in ("audienceId", "baseline", "watermark", "passSince"):
            text = value.get(name)
            if text is not None and not isinstance(text, str):
                raise ValueError("Mailchimp memberChanged cursor is not valid JSON")
            texts[name] = text or ""
        offset = value.get("passOffset")
        if offset is not None and (isinstance(offset, bool) or not isinstance(offset, int)):
            raise ValueError("Mailchimp memberChanged cursor is not valid JSON")
        position = _MemberPollCursor(
            texts["audienceId"], texts["baseline"], texts["watermark"], texts["passSince"], offset or 0
        )
        if position.audience_id != self._configuration.audience_id:
            raise ValueError("Mailchimp memberChanged cursor belongs to another audience")
        for text in (position.baseline, position.watermark, position.pass_since):
            _parse_cursor_time(text)
        if position.pass_offset < 0 or position.pass_offset > MAXIMUM_MEMBER_POLL_OFFSET:
            raise ValueError("Mailchimp memberChanged cursor offset is out of range")
        return position

    async def _list_members(self, context: TriggerContext, query: dict[str, str], offset: int) -> _MemberPage:
        """Send one GET /lists/{list_id}/members and classify its failure for the kit."""
        await wait_for_poll_request(context)
        connection = await self._client.resolve_credentials(self._call, _OPERATION)
        if not isinstance(connection, tuple):
            raise RuntimeError(connection.message)
        exchange = await self._client.exchange(
            self._call,
            _OPERATION,
            connection,
            "GET",
            f"/lists/{self._configuration.audience_id}/members",
            query=query,
        )
        context.cancellation.raise_if_cancelled()
        message = exchange.failure.message if exchange.failure is not None else "Mailchimp request failed"
        if exchange.outcome == "succeeded":
            try:
                return _decode_member_page(exchange.body)
            except (ValueError, TypeError, KeyError) as error:
                raise RuntimeError(f"Mailchimp returned an invalid page of contacts: {error}") from None
        if exchange.outcome == "rateLimited":
            raise mark_rate_limited(RuntimeError(message), timedelta(seconds=exchange.retry_after))
        if exchange.outcome == "rejected" and exchange.status == 400:
            raise mark_cursor_invalid(RuntimeError(message))
        raise RuntimeError(message)


def _decode_member_page(body: bytes) -> _MemberPage:
    page = _decode_object(body)
    members = page.get("members")
    if members is None:
        members = []
    total_items = page.get("total_items")
    if (
        not isinstance(members, list)
        or isinstance(total_items, bool)
        or not isinstance(total_items, int)
        or total_items < 0
        or len(members) > MEMBER_POLL_PAGE_SIZE
    ):
        raise ValueError("the page has no total_items or more contacts than requested")
    return _MemberPage(tuple(_decode_member(member) for member in members), total_items)


def _continue_pass(
    since: datetime | None, offset: int, page_length: int, listed: list[Member]
) -> tuple[str, int]:
    """Restart one second before the page's last change, or advance by offset within one second."""
    if listed:
        last_changed = listed[-1].last_changed_at
        first_changed = listed[0].last_changed_at
        assert last_changed is not None and first_changed is not None
        restart = last_changed - _SECOND
        if first_changed < last_changed and (since is None or restart > since):
            return _format_cursor_time(restart), 0
    if offset + page_length > MAXIMUM_MEMBER_POLL_OFFSET:
        raise RuntimeError(
            f"more than {MAXIMUM_MEMBER_POLL_OFFSET} Mailchimp contacts changed within one second"
        )
    return (_format_cursor_time(since) if since is not None else ""), offset + page_length


def _format_cursor_time(value: datetime) -> str:
    return value.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def _parse_cursor_time(value: str) -> datetime | None:
    if not value:
        return None
    try:
        if _CURSOR_TIME.fullmatch(value) is None:
            raise ValueError
        return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(UTC)
    except ValueError:
        raise ValueError("Mailchimp memberChanged cursor holds an invalid time") from None
