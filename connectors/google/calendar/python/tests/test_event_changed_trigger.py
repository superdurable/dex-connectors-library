# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""eventChanged sync fetcher parity with the Go connector's event_sync_fetcher_test.go."""

import json
from collections.abc import AsyncIterator
from datetime import UTC, datetime, timedelta
from email.utils import format_datetime

import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerContext
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import encode_trigger_event

from dex_connectors.google_calendar import (
    DEFAULT_TRIGGER_CALENDAR_ID,
    Client,
    Config,
    Credentials,
    Event,
    EventChangedEvent,
    EventChangedTriggerConfiguration,
    EventChangeKind,
    validate_event_changed_trigger_configuration,
    watched_calendar_id,
)
from dex_connectors.google_calendar.triggers import (
    _decode_event_changed_payload,
    _decode_event_sync_page,
    _event_change_kind,
    _EventSyncCursor,
    _EventSyncFetcher,
    _new_event_changed_trigger_event,
)
from tests.calendar_sync_fake import CalendarSyncFake

SYNC_CALENDAR_ID = "team@group.calendar.google.com"
REFERENCE = ConnectionRef("google", "calendar-sync")
CONTEXT = TriggerContext()


@pytest.fixture
async def fake() -> AsyncIterator[CalendarSyncFake]:
    yield CalendarSyncFake(SYNC_CALENDAR_ID)


def new_client(fake: CalendarSyncFake, http_client: httpx.AsyncClient) -> Client:
    credentials = Credentials(access_token=SecretString("calendar-token"))
    return Client(
        Config(endpoint="http://127.0.0.1/calendar/v3"),
        StaticCredentialProvider({REFERENCE: credentials}),
        http_client,
    )


@pytest.fixture
async def http_client(fake: CalendarSyncFake) -> AsyncIterator[httpx.AsyncClient]:
    async with httpx.AsyncClient(transport=fake.transport()) as client:
        yield client


def new_fetcher(
    fake: CalendarSyncFake, http_client: httpx.AsyncClient, calendar_id: str = SYNC_CALENDAR_ID
) -> _EventSyncFetcher:
    return _EventSyncFetcher(new_client(fake, http_client), REFERENCE, calendar_id)


def decode_cursor(cursor: Cursor) -> dict[str, str]:
    decoded: dict[str, str] = json.loads(cursor.token)
    return decoded


def test_cursor_documents_and_event_ids_match_the_go_connector_byte_for_byte() -> None:
    # Printed by the Go connector's encodeCursor and newEventChangedTriggerEvent.
    plain = _EventSyncCursor(SYNC_CALENDAR_ID, "sync-1").encode()
    assert plain.token == '{"calendarId":"team@group.calendar.google.com","syncToken":"sync-1"}'
    escaped = _EventSyncCursor("a<b>&c é", "CPDA/x+y==", "page-250-260").encode()
    assert escaped.token == (
        '{"calendarId":"a\\u003cb\\u003e\\u0026c\\u2028é","syncToken":"CPDA/x+y==","pageToken":"page-250-260"}'
    )
    assert _EventSyncCursor.decode(escaped) == _EventSyncCursor("a<b>&c é", "CPDA/x+y==", "page-250-260")
    epoch = datetime(1970, 1, 1, tzinfo=UTC)
    cases = [
        (
            Event(
                "kickoff0001",
                etag='"3"',
                status="confirmed",
                created="2026-03-02T09:03:00Z",
                updated="2026-03-02T09:03:00Z",
            ),
            "gcal-9705eeeae88ed6742e3c44b195356bd6",
            "created",
            datetime(2026, 3, 2, 9, 3, tzinfo=UTC),
        ),
        (
            Event("review00001", etag='"5"', status="cancelled", updated="2026-03-02T09:05:00Z"),
            "gcal-9131190cb3e47cb91d3442ebe907b851",
            "cancelled",
            datetime(2026, 3, 2, 9, 5, tzinfo=UTC),
        ),
        (
            Event(
                "x",
                status="confirmed",
                created="2026-03-02T09:00:00.000Z",
                updated="2026-03-02T09:00:01.000000001Z",
            ),
            "gcal-b5a258445a2a42235430d1d1e48f0183",
            "updated",
            datetime(2026, 3, 2, 9, 0, 1, tzinfo=UTC),
        ),
        (
            Event(
                "y", status="confirmed", created="2026-03-02T10:00:00+01:00", updated="2026-03-02T09:00:00.9Z"
            ),
            "gcal-235fa0124c00f9dbb92cc0d2c3374c9e",
            "created",
            datetime(2026, 3, 2, 9, 0, 0, 900000, tzinfo=UTC),
        ),
    ]
    for event, event_id, kind, occurred_at in cases:
        change = _new_event_changed_trigger_event(SYNC_CALENDAR_ID, event, epoch)
        assert (change.id, change.payload.change_kind, change.occurred_at) == (event_id, kind, occurred_at)


def test_a_change_recorded_by_a_go_replica_replays_in_python() -> None:
    # Printed by the Go connector's json.Marshal of an EventChangedEvent.
    go_payload = (
        '{"calendarId":"team@group.calendar.google.com","changeKind":"created","event":{"id":"kickoff0001",'
        '"calendarId":"team@group.calendar.google.com","status":"confirmed","summary":"Kickoff",'
        '"start":{"dateTime":"2026-03-10T10:00:00Z"},"end":{"dateTime":"2026-03-10T11:00:00Z"},'
        '"isAllDay":false,"transparency":"opaque","etag":"\\"3\\"","created":"2026-03-02T09:03:00Z",'
        '"updated":"2026-03-02T09:03:00Z"}}'
    )
    decoded = _decode_event_changed_payload(go_payload, go_payload.encode())
    assert decoded.change_kind == "created"
    assert (decoded.event.summary, decoded.event.etag, decoded.event.start.date_time) == (
        "Kickoff",
        '"3"',
        "2026-03-10T10:00:00Z",
    )
    change = _new_event_changed_trigger_event(SYNC_CALENDAR_ID, decoded.event, datetime.now(UTC))
    stored = encode_trigger_event(change)
    payload = json.dumps(json.loads(stored)["payload"], separators=(",", ":"))
    assert _decode_event_changed_payload(payload, payload.encode()) == decoded
    assert json.loads(payload) == json.loads(go_payload)


async def test_baseline_pages_through_the_full_sync_and_keeps_only_the_sync_token(
    fake: CalendarSyncFake, http_client: httpx.AsyncClient
) -> None:
    for index in range(260):
        fake.insert(f"existing{index}", "Existing")
    cursor = await new_fetcher(fake, http_client).seed_cursor(CONTEXT)

    assert decode_cursor(cursor) == {"calendarId": SYNC_CALENDAR_ID, "syncToken": "sync-260"}
    assert [request.query for request in fake.requests()] == [
        "maxResults=250",
        "maxResults=250&pageToken=page-250-260",
    ]


async def test_fetch_reports_created_updated_and_cancelled_events_with_stable_ids(
    fake: CalendarSyncFake, http_client: httpx.AsyncClient
) -> None:
    fake.insert("standup0001", "Standup")
    fake.insert("review00001", "Review")
    fetcher = new_fetcher(fake, http_client)
    cursor = await fetcher.seed_cursor(CONTEXT)
    fake.insert("kickoff0001", "Kickoff")
    fake.update("standup0001", "Standup moved")
    fake.cancel("review00001")

    page = await fetcher.fetch_changes(CONTEXT, cursor)

    assert not page.has_more_pages
    assert [change.payload.calendar_id for change in page.changes] == [SYNC_CALENDAR_ID] * 3
    assert [change.payload.change_kind for change in page.changes] == ["created", "updated", "cancelled"]
    assert page.changes[0].payload.event.summary == "Kickoff"
    assert page.changes[1].payload.event.summary == "Standup moved"
    assert page.changes[2].payload.event.id == "review00001"
    assert page.changes[1].occurred_at == datetime(2026, 3, 2, 9, 4, tzinfo=UTC)
    assert decode_cursor(page.next_cursor) == {"calendarId": SYNC_CALENDAR_ID, "syncToken": "sync-5"}
    assert fake.requests()[-1].query == "maxResults=250&syncToken=sync-2"

    refetched = await fetcher.fetch_changes(CONTEXT, cursor)
    assert [change.id for change in refetched.changes] == [change.id for change in page.changes]
    assert page.changes[0].id != page.changes[1].id


async def test_fetch_repeats_the_sync_token_with_each_page_token(
    fake: CalendarSyncFake, http_client: httpx.AsyncClient
) -> None:
    fetcher = new_fetcher(fake, http_client)
    cursor = await fetcher.seed_cursor(CONTEXT)
    for index in range(260):
        fake.insert(f"burst{index}", "Burst")

    first = await fetcher.fetch_changes(CONTEXT, cursor)
    assert first.has_more_pages and len(first.changes) == 250
    assert first.next_cursor.token == (
        '{"calendarId":"team@group.calendar.google.com","syncToken":"sync-0","pageToken":"page-250-260"}'
    )
    second = await fetcher.fetch_changes(CONTEXT, first.next_cursor)

    assert not second.has_more_pages and len(second.changes) == 10
    assert decode_cursor(second.next_cursor) == {"calendarId": SYNC_CALENDAR_ID, "syncToken": "sync-260"}
    assert fake.requests()[-1].query == "maxResults=250&pageToken=page-250-260&syncToken=sync-0"


async def test_fetch_marks_an_expired_foreign_or_undecodable_cursor_invalid(
    fake: CalendarSyncFake, http_client: httpx.AsyncClient
) -> None:
    fetcher = new_fetcher(fake, http_client)
    cursor = await fetcher.seed_cursor(CONTEXT)
    fake.expire_sync_tokens()

    with pytest.raises(Exception) as expired:
        await fetcher.fetch_changes(CONTEXT, cursor)
    assert is_cursor_invalid(expired.value), "410 Gone requires a new full sync"
    with pytest.raises(Exception) as foreign:
        await new_fetcher(fake, http_client, "primary").fetch_changes(CONTEXT, cursor)
    assert is_cursor_invalid(foreign.value), "a binding moved to another calendar records a new baseline"
    for token in ("not-json", "[]", '{"calendarId":"team@group.calendar.google.com"}', '{"syncToken":5}'):
        with pytest.raises(Exception) as undecodable:
            await fetcher.fetch_changes(CONTEXT, Cursor(token))
        assert is_cursor_invalid(undecodable.value), token


async def test_fetch_reports_rate_limits_and_other_failures_without_resetting(
    fake: CalendarSyncFake, http_client: httpx.AsyncClient
) -> None:
    fetcher = new_fetcher(fake, http_client)
    cursor = await fetcher.seed_cursor(CONTEXT)
    fake.rate_limit_next_requests(1, "7")
    with pytest.raises(RateLimitedError) as limited:
        await fetcher.fetch_changes(CONTEXT, cursor)
    assert limited.value.retry_after == timedelta(seconds=7)
    assert not is_cursor_invalid(limited.value)

    for reason in ("userRateLimitExceeded", "rateLimitExceeded", "quotaExceeded", "dailyLimitExceeded"):
        fake.throttle_next_requests(1, 403, reason, "")
        with pytest.raises(RateLimitedError) as throttled:
            await fetcher.fetch_changes(CONTEXT, cursor)
        assert throttled.value.retry_after == timedelta(0), "no Retry-After backs off exponentially"
        assert reason in str(throttled.value)
    # Google may send an HTTP date; the operation path rejects it, but a poll still pauses for it.
    fake.rate_limit_next_requests(1, format_datetime(datetime.now(UTC) + timedelta(hours=1), usegmt=True))
    with pytest.raises(RateLimitedError) as dated:
        await fetcher.fetch_changes(CONTEXT, cursor)
    assert dated.value.retry_after > timedelta(minutes=30)
    fake.throttle_next_requests(1, 403, "forbidden", "")
    with pytest.raises(RuntimeError) as forbidden:
        await fetcher.fetch_changes(CONTEXT, cursor)
    assert not isinstance(forbidden.value, RateLimitedError), "a plain 403 backs off per binding"
    assert not is_cursor_invalid(forbidden.value)
    assert (
        str(forbidden.value)
        == "Google Calendar sync request: provider rejected the request with HTTP 403 (forbidden)"
    )

    missing = new_fetcher(fake, http_client, "deleted@group.calendar.google.com")
    with pytest.raises(RuntimeError) as not_found:
        await missing.seed_cursor(CONTEXT)
    assert not is_cursor_invalid(not_found.value), "a missing calendar is retried, not reseeded"
    assert "calendar-token" not in str(not_found.value)


async def test_fetch_refuses_a_page_without_a_continuation_and_unavailable_credentials(
    http_client: httpx.AsyncClient,
) -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"items": []})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as broken:
        fetcher = _EventSyncFetcher(new_client(CalendarSyncFake("x"), broken), REFERENCE, SYNC_CALENDAR_ID)
        with pytest.raises(RuntimeError, match="neither a new page token nor a sync token"):
            await fetcher.seed_cursor(CONTEXT)
        with pytest.raises(RuntimeError, match="neither a page token nor a sync token"):
            await fetcher.fetch_changes(CONTEXT, _EventSyncCursor(SYNC_CALENDAR_ID, "sync-1").encode())
    unauthorized = Client(
        Config(endpoint="http://127.0.0.1/calendar/v3"), StaticCredentialProvider({}), http_client
    )
    with pytest.raises(RuntimeError, match="credentials are unavailable"):
        await _EventSyncFetcher(unauthorized, REFERENCE, SYNC_CALENDAR_ID).seed_cursor(CONTEXT)


def test_event_change_kind_is_derived_from_created_and_updated_times() -> None:
    created = Event(
        "a", status="confirmed", created="2026-03-02T09:00:00.000Z", updated="2026-03-02T09:00:00.640Z"
    )
    assert _event_change_kind(created) is EventChangeKind.CREATED
    updated = Event("a", status="confirmed", created="2026-03-02T09:00:00Z", updated="2026-03-02T09:00:02Z")
    assert _event_change_kind(updated) is EventChangeKind.UPDATED
    assert _event_change_kind(Event("a", status="tentative")) is EventChangeKind.UPDATED
    cancelled = Event("a", status="cancelled", created="2026-03-02T09:00:00Z", updated="2026-03-02T09:00:00Z")
    assert _event_change_kind(cancelled) is EventChangeKind.CANCELLED


def test_configuration_defaults_to_the_primary_calendar_and_poll_interval_has_a_floor() -> None:
    assert watched_calendar_id(EventChangedTriggerConfiguration()) == DEFAULT_TRIGGER_CALENDAR_ID == "primary"
    assert (
        watched_calendar_id(EventChangedTriggerConfiguration(calendar_id=SYNC_CALENDAR_ID))
        == SYNC_CALENDAR_ID
    )
    validate_event_changed_trigger_configuration(EventChangedTriggerConfiguration())
    with pytest.raises(ValueError, match="surrounding whitespace"):
        validate_event_changed_trigger_configuration(EventChangedTriggerConfiguration(calendar_id=" primary"))
    with pytest.raises(ValueError, match="pollInterval must be at least 5s"):
        Client(Config(poll_interval_seconds=1), StaticCredentialProvider({}))
    assert (
        Client(Config(poll_interval_seconds=0), StaticCredentialProvider({})).config.poll_interval_seconds
        == 30
    )
    assert isinstance(EventChangedEvent(change_kind="created"), EventChangedEvent)


def test_sync_page_decode_rejects_wrong_types_as_go_json_does() -> None:
    page = _decode_event_sync_page(b'{"nextPageToken":null,"nextSyncToken":"s","items":null}')
    assert (page.next_page_token, page.next_sync_token, page.items) == ("", "s", [])
    for body in (b'{"nextSyncToken":0}', b'{"nextPageToken":false}', b'{"items":{}}', b'{"items":false}'):
        with pytest.raises(RuntimeError, match="not valid JSON"):
            _decode_event_sync_page(body)
