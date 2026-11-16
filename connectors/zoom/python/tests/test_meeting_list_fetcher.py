# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""meetingChanged fetcher behavior, ported from the Go connector's meeting_list_fetcher tests."""

import json
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from dataclasses import replace
from datetime import timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
)
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import KeyedSnapshot

from dex_connectors.zoom import (
    MEETING_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    MeetingChangedTriggerConfiguration,
    MeetingSummary,
    validate_meeting_changed_trigger_configuration,
)
from dex_connectors.zoom._meeting_list_fetcher import MEETING_CHANGES_PER_PAGE, MeetingListFetcher
from dex_connectors.zoom._meeting_snapshot import ListedMeeting
from tests.zoom_meetings_fake import ACCESS_TOKEN, ZoomMeetingsFake

HOST_ID = "zoom-host-1"
REFERENCE = ConnectionRef("zoom", "zoom-watcher")
CONTEXT = TriggerContext(TriggerCancellation())


def new_client(http_client: httpx.AsyncClient, poll_interval_seconds: float = 30) -> Client:
    """A client with the fake's bearer token."""
    empty = SecretString("")
    credentials = Credentials("", empty, SecretString(ACCESS_TOKEN), empty)
    return Client(
        Config(endpoint="https://zoom.example/v2", poll_interval_seconds=poll_interval_seconds),
        StaticCredentialProvider({REFERENCE: credentials}),
        http_client,
    )


@asynccontextmanager
async def fetcher_for(
    fake: ZoomMeetingsFake, configuration: MeetingChangedTriggerConfiguration | None = None
) -> AsyncIterator[MeetingListFetcher]:
    """A fetcher whose requests the fake answers."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(fake.handle)) as http_client:
        yield MeetingListFetcher(
            new_client(http_client),
            REFERENCE,
            MEETING_CHANGED_TRIGGER_DEFINITION.trigger,
            configuration or MeetingChangedTriggerConfiguration(),
        )


def committed(cursor: Cursor, commit_count: int) -> Cursor:
    """Emulate the kit's commit, which stamps new entries with the commit count."""
    assert cursor.snapshot is not None
    entries = tuple(replace(entry, seen=entry.seen or commit_count) for entry in cursor.snapshot.entries)
    return Cursor(cursor.token, KeyedSnapshot(entries))


async def test_seeds_without_backlog_then_delivers_each_difference() -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    fake.schedule(92674392836, "Consultation", 1)
    fake.schedule(92674392840, "Team review", 2)
    fake.schedule(92674392841, "Planning", 3)
    async with fetcher_for(fake) as fetcher:
        seed = await fetcher.seed_cursor(CONTEXT)
        assert json.loads(seed.token) == {"version": 1, "hostId": HOST_ID}
        assert seed.snapshot is not None and len(seed.snapshot.entries) == 3
        assert b"join" not in (seed.snapshot.entries[0].value or b""), "the snapshot never keeps the join URL"
        unchanged = await fetcher.fetch_changes(CONTEXT, seed)
        assert not unchanged.changes and not unchanged.has_more_pages, "the baseline is never delivered"

        fake.schedule(92674392850, "Kickoff", 4)
        fake.reschedule(92674392840, 2)
        fake.delete(92674392841)
        page = await fetcher.fetch_changes(CONTEXT, seed)
        assert [(change.payload.meeting.id, change.payload.change_kind) for change in page.changes] == [
            (92674392840, "updated"),
            (92674392841, "removed"),
            (92674392850, "scheduled"),
        ], "changes arrive in meeting ID order"
        updated = page.changes[0].payload
        assert updated.previous_meeting is not None
        assert updated.previous_meeting.start_time == "2036-02-25T17:00:00Z"
        assert updated.meeting.start_time == "2036-02-25T19:00:00Z"
        assert updated.meeting.join_url == "https://us05web.zoom.us/j/92674392840"
        removed = page.changes[1].payload
        assert removed.meeting.topic == "Planning", "a removal carries the last listed state"
        assert removed.previous_meeting is None and not removed.meeting.join_url
        scheduled = page.changes[2]
        assert scheduled.occurred_at.isoformat() == "2036-02-23T13:00:00+00:00", "it occurs when created"

        refetched = await fetcher.fetch_changes(CONTEXT, seed)
        assert [change.id for change in refetched.changes] == [change.id for change in page.changes]
        after = await fetcher.fetch_changes(CONTEXT, page.next_cursor)
        assert not after.changes


async def test_gives_a_recovered_meeting_a_new_scheduled_id() -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    async with fetcher_for(fake) as fetcher:
        cursor = committed(await fetcher.seed_cursor(CONTEXT), 1)
        commits = 1

        async def poll() -> list[tuple[str, str, MeetingSummary]]:
            nonlocal cursor, commits
            page = await fetcher.fetch_changes(CONTEXT, cursor)
            commits += 1
            cursor = committed(page.next_cursor, commits)
            return [
                (change.id, change.payload.change_kind, change.payload.meeting) for change in page.changes
            ]

        fake.schedule(92674392836, "Consultation", 1)
        scheduled = await poll()
        assert len(scheduled) == 1
        assert await poll() == []
        assert json.loads(cursor.token) == {"version": 1, "hostId": HOST_ID, "sequence": 1}
        fake.delete(92674392836)
        assert (await poll())[0][1] == "removed"
        fake.recover(92674392836)
        recovered = await poll()
        assert len(recovered) == 1 and recovered[0][1] == "scheduled"
        assert recovered[0][2] == scheduled[0][2], "Zoom restores the same meeting"
        assert recovered[0][0] != scheduled[0][0], (
            "a recovery is not deduplicated against the first scheduling"
        )


def test_listed_meeting_duplicates_keep_the_earliest_start() -> None:
    def listed(start: str | None, topic: str) -> ListedMeeting:
        return ListedMeeting.from_summary(
            MeetingSummary(id=92674392836, topic=topic, type=8 if start else 3, start_time=start)
        )

    next_occurrence = listed("2036-03-02T17:00:00Z", "Weekly sync")
    later = listed("2036-03-09T17:00:00Z", "Weekly sync")
    assert next_occurrence.lists_before(later) and not later.lists_before(next_occurrence)
    assert later.lists_before(listed(None, "Weekly sync")), "a listed start wins over none"
    first, second = listed("2036-03-02T17:00:00Z", "A"), listed("2036-03-02T17:00:00Z", "B")
    assert first.lists_before(second) != second.lists_before(first), "equal starts order by hash"
    fraction = listed("2036-03-02T17:00:00.5Z", "Weekly sync")
    assert next_occurrence.lists_before(fraction), "fractional seconds compare as instants"


async def test_skips_unconfigured_kinds_but_advances_past_them() -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    fake.schedule(92674392836, "Consultation", 1)
    async with fetcher_for(fake, MeetingChangedTriggerConfiguration(change_kinds=("removed",))) as fetcher:
        seed = await fetcher.seed_cursor(CONTEXT)
        fake.rename(92674392836, "Consultation moved")
        fake.schedule(92674392850, "Kickoff", 4)
        page = await fetcher.fetch_changes(CONTEXT, seed)
        assert not page.changes
        assert page.next_cursor.snapshot is not None
        assert len(page.next_cursor.snapshot.entries) == 2, "skipped changes still advance the snapshot"
        fake.delete(92674392850)
        page = await fetcher.fetch_changes(CONTEXT, page.next_cursor)
        assert [(change.payload.meeting.id, change.payload.change_kind) for change in page.changes] == [
            (92674392850, "removed")
        ]


async def test_reads_every_page_and_restarts_after_a_rejected_page_token() -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    for index in range(650):
        fake.schedule(80000000000 + index, "Office hours", index % 30)
    async with fetcher_for(fake) as fetcher:
        seed = await fetcher.seed_cursor(CONTEXT)
        assert seed.snapshot is not None and len(seed.snapshot.entries) == 650
        assert fake.listing_request_count() == 3, "Zoom's largest page size is used"
        assert all(request.query["type"] == "scheduled" for request in fake.requests)
        assert all(request.query["page_size"] == "300" for request in fake.requests)

        fake.reject_next_page_tokens(1)
        page = await fetcher.fetch_changes(CONTEXT, seed)
        assert not page.changes, "an expired page token restarts the listing instead of reporting removals"
        assert fake.listing_request_count() == 3 + 2 + 3

        fake.reject_next_page_tokens(2)
        with pytest.raises(RuntimeError, match="page token") as raised:
            await fetcher.fetch_changes(CONTEXT, seed)
        assert not is_cursor_invalid(raised.value), "the stored snapshot stays valid"


async def test_splits_large_differences_across_commits() -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    async with fetcher_for(fake) as fetcher:
        seed = await fetcher.seed_cursor(CONTEXT)
        assert json.loads(seed.token) == {"version": 1}, "an empty list names no host"
        for index in range(250):
            fake.schedule(80000000000 + index, "Office hours", 1)
        first = await fetcher.fetch_changes(CONTEXT, seed)
        assert len(first.changes) == MEETING_CHANGES_PER_PAGE and first.has_more_pages
        assert first.next_cursor.snapshot is not None
        assert len(first.next_cursor.snapshot.entries) == MEETING_CHANGES_PER_PAGE
        assert json.loads(first.next_cursor.token) == {"version": 1, "hostId": HOST_ID, "sequence": 1}
        delivered = {change.id for change in first.changes}
        cursor = first.next_cursor
        for _ in range(2):
            page = await fetcher.fetch_changes(CONTEXT, cursor)
            assert not delivered & {change.id for change in page.changes}, "no change is repeated"
            delivered |= {change.id for change in page.changes}
            cursor = page.next_cursor
        assert len(delivered) == 250


@pytest.mark.parametrize("is_daily", [False, True])
async def test_maps_rate_limits_to_the_kit_signal(is_daily: bool) -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    fake.schedule(92674392836, "Consultation", 1)
    async with fetcher_for(fake) as fetcher:
        seed = await fetcher.seed_cursor(CONTEXT)
        fake.rate_limit_next(1, is_daily, "7")
        with pytest.raises(RateLimitedError) as limited:
            await fetcher.fetch_changes(CONTEXT, seed)
        assert limited.value.retry_after == timedelta(seconds=7)
        fake.rate_limit_next(1, is_daily, "")
        with pytest.raises(RateLimitedError) as unhinted:
            await fetcher.fetch_changes(CONTEXT, seed)
        assert unhinted.value.retry_after == timedelta(0), "without Retry-After the runner backs off"


async def test_marks_undecodable_and_foreign_cursors_invalid() -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    fake.schedule(92674392836, "Consultation", 1)
    async with fetcher_for(fake) as fetcher:
        seed = await fetcher.seed_cursor(CONTEXT)
        for name, cursor in {
            "undecodable": Cursor("sync-1", seed.snapshot),
            "version": Cursor('{"version":2}', seed.snapshot),
            "string version": Cursor('{"version":"1"}', seed.snapshot),
            "no snapshot": Cursor(seed.token),
            "other user": Cursor('{"version":1,"hostId":"another-host"}', seed.snapshot),
        }.items():
            with pytest.raises(Exception) as raised:
                await fetcher.fetch_changes(CONTEXT, cursor)
            assert is_cursor_invalid(raised.value), name


async def test_reports_unavailable_credentials_and_provider_failures_without_secrets() -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    async with httpx.AsyncClient(transport=httpx.MockTransport(fake.handle)) as http_client:
        fetcher = MeetingListFetcher(
            Client(Config(endpoint="https://zoom.example/v2"), StaticCredentialProvider({}), http_client),
            REFERENCE,
            MEETING_CHANGED_TRIGGER_DEFINITION.trigger,
            MeetingChangedTriggerConfiguration(),
        )
        with pytest.raises(RuntimeError, match="Zoom Trigger credentials are unavailable"):
            await fetcher.seed_cursor(CONTEXT)
    empty = SecretString("")
    wrong = Credentials("", empty, SecretString("rejected-token"), empty)
    async with httpx.AsyncClient(transport=httpx.MockTransport(fake.handle)) as http_client:
        fetcher = MeetingListFetcher(
            Client(
                Config(endpoint="https://zoom.example/v2"),
                StaticCredentialProvider({REFERENCE: wrong}),
                http_client,
            ),
            REFERENCE,
            MEETING_CHANGED_TRIGGER_DEFINITION.trigger,
            MeetingChangedTriggerConfiguration(),
        )
        with pytest.raises(RuntimeError, match="HTTP 401") as raised:
            await fetcher.seed_cursor(CONTEXT)
        assert "rejected-token" not in str(raised.value)


def test_validates_change_kinds_and_the_poll_interval() -> None:
    validate_meeting_changed_trigger_configuration(MeetingChangedTriggerConfiguration())
    validate_meeting_changed_trigger_configuration(
        MeetingChangedTriggerConfiguration(change_kinds=("scheduled", "removed"))
    )
    with pytest.raises(ValueError, match="must be scheduled"):
        validate_meeting_changed_trigger_configuration(
            MeetingChangedTriggerConfiguration(change_kinds=("ended",))
        )
    with pytest.raises(ValueError, match="repeats"):
        validate_meeting_changed_trigger_configuration(
            MeetingChangedTriggerConfiguration(change_kinds=("updated", "updated"))
        )
    with pytest.raises(ValueError, match="pollInterval must be at least 30s"):
        Client(Config(poll_interval_seconds=10), StaticCredentialProvider({}))
    assert Client(Config(), StaticCredentialProvider({})).poll_interval == timedelta(minutes=2)
