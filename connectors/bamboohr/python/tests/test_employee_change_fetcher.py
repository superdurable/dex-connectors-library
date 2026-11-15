# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The employeeChanged fetcher, ported from the Go connector's employee_change_fetcher tests."""

import json
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from datetime import datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    Cursor,
    RateLimitedError,
    is_cursor_invalid,
)

from dex_connectors.bamboohr import (
    EMPLOYEE_CHANGE_SETTLE_WINDOW,
    Client,
    Config,
    Credentials,
    EmployeeChangedEvent,
    EmployeeChangedTriggerConfiguration,
    validate_employee_changed_trigger_configuration,
)
from dex_connectors.bamboohr._employee_change_fetcher import EmployeeChangeFetcher
from tests.bamboohr_fake import API_KEY, START, BambooHRFake

REFERENCE = ConnectionRef("bamboohr", "bamboohr-trigger-test")
CONTEXT = TriggerContext(TriggerCancellation())


def new_client(
    http_client: httpx.AsyncClient, max_response_bytes: int = 4194304, **options: object
) -> Client:
    """A client for the fake company acme."""
    credentials = StaticCredentialProvider({REFERENCE: Credentials(SecretString(API_KEY))})
    config = Config(company_domain="acme", max_response_bytes=max_response_bytes, poll_interval_seconds=10)
    return Client(config, credentials, http_client, "https://acme.example/api/v1", **options)  # type: ignore[arg-type]


@asynccontextmanager
async def fetcher_for(
    handler: object, change_type: str = "", max_response_bytes: int = 4194304, clock: object = None
) -> AsyncIterator[EmployeeChangeFetcher]:
    """A fetcher whose requests handler answers."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http_client:  # type: ignore[arg-type]
        client = new_client(http_client, max_response_bytes, clock=clock)
        configuration = EmployeeChangedTriggerConfiguration(change_type=change_type or None)
        yield EmployeeChangeFetcher(client, REFERENCE, configuration)


def event_ids(events: object) -> list[str]:
    """The IDs of the delivered events, in order."""
    return [event.id for event in events]  # type: ignore[attr-defined]


async def test_seed_starts_at_bamboohr_clock_without_backlog() -> None:
    fake = BambooHRFake()
    fake.insert("140", {"firstName": "Ava"})
    fake.advance(timedelta(seconds=1))
    # The local clock runs an hour ahead; the cursor must still start at BambooHR's time.
    async with fetcher_for(fake.handle, clock=lambda: START + timedelta(hours=1)) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        assert cursor.token == '{"v":1,"since":"2026-10-13T09:00:02Z"}'
        fake.advance(timedelta(minutes=1))
        page = await fetcher.fetch_changes(CONTEXT, cursor)
    assert not page.changes, "the employee inserted before the seed is not delivered"
    assert page.next_cursor.token == cursor.token
    assert [query["since"] for query in fake.change_list_queries] == [
        "2026-10-13T09:59:59Z",
        "2026-10-13T09:00:01Z",
    ], "since is one second before the cursor"


async def test_fetch_delivers_changes_oldest_first_with_stable_ids() -> None:
    fake = BambooHRFake()
    async with fetcher_for(fake.handle) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        fake.insert("141", {"firstName": "Sam"})
        fake.update("9", {"department": "Sales"})
        fake.delete("77")
        fake.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
        page = await fetcher.fetch_changes(CONTEXT, cursor)
        assert not page.has_more_pages
        assert event_ids(page.changes) == [
            "employee-141-inserted-1791882001",
            "employee-9-updated-1791882002",
            "employee-77-deleted-1791882003",
        ]
        changed: TriggerEvent[EmployeeChangedEvent] = page.changes[1]
        assert changed.payload == EmployeeChangedEvent(
            "9", action="Updated", last_changed=START + timedelta(seconds=2)
        )
        assert changed.occurred_at == changed.payload.last_changed
        assert page.next_cursor.token == '{"v":1,"since":"2026-10-13T09:00:03Z","afterEmployeeId":"77"}'
        refetched = await fetcher.fetch_changes(CONTEXT, cursor)
        assert event_ids(refetched.changes) == event_ids(page.changes), "a refetch keeps the event IDs"
        assert not (await fetcher.fetch_changes(CONTEXT, page.next_cursor)).changes


async def test_fetch_waits_for_changes_to_settle() -> None:
    fake = BambooHRFake()
    async with fetcher_for(fake.handle) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        fake.insert("200")
        fake.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW - timedelta(seconds=2))
        page = await fetcher.fetch_changes(CONTEXT, cursor)
        assert not page.changes, "a change newer than the settle window waits"
        assert page.next_cursor.token == cursor.token
        # Employee 150 shares employee 200's second but becomes visible only now.
        fake.insert_late("150", START + timedelta(seconds=1))
        fake.advance(timedelta(seconds=2))
        page = await fetcher.fetch_changes(CONTEXT, page.next_cursor)
    assert event_ids(page.changes) == ["employee-150-inserted-1791882001", "employee-200-inserted-1791882001"]
    assert page.next_cursor.token == '{"v":1,"since":"2026-10-13T09:00:01Z","afterEmployeeId":"200"}'


async def test_fetch_sends_the_configured_change_type() -> None:
    fake = BambooHRFake()
    async with fetcher_for(fake.handle, "inserted") as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        assert cursor.token == '{"v":1,"since":"2026-10-13T09:00:00Z","changeType":"inserted"}'
        fake.insert("141")
        fake.update("9")
        fake.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
        page = await fetcher.fetch_changes(CONTEXT, cursor)
    assert event_ids(page.changes) == ["employee-141-inserted-1791882001"]
    assert all(query["type"] == "inserted" for query in fake.change_list_queries)


async def test_fetch_pages_a_change_list_larger_than_one_page() -> None:
    fake = BambooHRFake()
    async with fetcher_for(fake.handle) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        for employee in range(1, MAXIMUM_CHANGES_PER_PAGE + 3):
            fake.insert(str(employee))
        fake.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
        page = await fetcher.fetch_changes(CONTEXT, cursor)
        assert len(page.changes) == MAXIMUM_CHANGES_PER_PAGE and page.has_more_pages
        rest = await fetcher.fetch_changes(CONTEXT, page.next_cursor)
    assert event_ids(rest.changes) == ["employee-501-inserted-1791882501", "employee-502-inserted-1791882502"]
    assert not rest.has_more_pages


async def test_fetch_marks_throttling_as_rate_limited() -> None:
    fake = BambooHRFake()
    async with fetcher_for(fake.handle) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        fake.rate_limit_change_lists(1)
        with pytest.raises(RateLimitedError) as raised:
            await fetcher.fetch_changes(CONTEXT, cursor)
    assert raised.value.retry_after == timedelta(seconds=1)
    assert not is_cursor_invalid(raised.value)


@pytest.mark.parametrize(
    ("status", "headers", "is_rate_limited", "is_invalid", "message"),
    [
        (503, {"Retry-After": "7"}, True, False, "HTTP 503"),
        (400, {}, False, True, "HTTP 400"),
        (401, {}, False, False, "HTTP 401"),
        (500, {}, False, False, "HTTP 500"),
    ],
)
async def test_fetch_status_classification(
    status: int, headers: dict[str, str], is_rate_limited: bool, is_invalid: bool, message: str
) -> None:
    async with fetcher_for(lambda request: httpx.Response(status, headers=headers)) as fetcher:
        with pytest.raises(Exception) as raised:
            await fetcher.fetch_changes(CONTEXT, Cursor(token='{"v":1,"since":"2026-10-13T09:00:00Z"}'))
    assert isinstance(raised.value, RateLimitedError) == is_rate_limited
    assert is_cursor_invalid(raised.value) == is_invalid
    assert message in str(raised.value) and API_KEY not in str(raised.value)
    if is_rate_limited:
        assert isinstance(raised.value, RateLimitedError)
        assert raised.value.retry_after == timedelta(seconds=7)


async def test_fetch_reports_an_oversized_change_list() -> None:
    fake = BambooHRFake()
    for employee in range(1, 21):
        fake.insert(str(employee))
    async with fetcher_for(fake.handle, max_response_bytes=256) as fetcher:
        with pytest.raises(RuntimeError, match="raise maxResponseBytes") as raised:
            await fetcher.fetch_changes(CONTEXT, Cursor(token='{"v":1,"since":"2026-10-13T09:00:00Z"}'))
    assert not is_cursor_invalid(raised.value), "an oversized list is a configuration fault, never a gap"


async def test_fetch_rejects_text_go_json_rejects() -> None:
    # Go's encoding/json rejects a byte order mark and non-JSON whitespace after the document.
    for body in (b"\xef\xbb\xbf" + b'{"employees":[]}', '{"employees":[]}\u00a0'.encode()):

        def handler(request: httpx.Request, body: bytes = body) -> httpx.Response:
            return httpx.Response(200, content=body, headers={"Date": "Tue, 13 Oct 2026 10:00:00 GMT"})

        async with fetcher_for(handler) as fetcher:
            with pytest.raises(RuntimeError, match="the response is not a change list"):
                await fetcher.fetch_changes(CONTEXT, Cursor(token='{"v":1,"since":"2026-10-13T09:00:00Z"}'))


async def test_fetch_rejects_foreign_cursors() -> None:
    fake = BambooHRFake()
    async with fetcher_for(fake.handle, "deleted") as fetcher:
        for token in (
            "",
            "not json",
            '{"v":2,"since":"2026-10-13T09:00:00Z","changeType":"deleted"}',
            '{"v":1,"since":"2026-10-13T09:00:00Z","changeType":"deleted","extra":1}',
            '{"v":1,"since":"2026-10-13T09:00:00Z","afterEmployeeId":"x1","changeType":"deleted"}',
            '{"v":1,"since":"2026-10-13T09:00:00Z","changeType":"updated"}',
            '{"v":1,"since":"2026-10-13T09:00:00Z"}',
        ):
            with pytest.raises(Exception) as raised:
                await fetcher.fetch_changes(CONTEXT, Cursor(token=token))
            assert is_cursor_invalid(raised.value), token
    assert not fake.change_list_queries, "an invalid cursor makes no request"


def test_employee_changed_configuration_validation() -> None:
    for change_type in (None, "", "inserted", "updated", "deleted"):
        validate_employee_changed_trigger_configuration(EmployeeChangedTriggerConfiguration(change_type))
    for change_type in ("all", "Inserted", " inserted"):
        with pytest.raises(ValueError, match="inserted, updated, deleted, or blank"):
            validate_employee_changed_trigger_configuration(EmployeeChangedTriggerConfiguration(change_type))


def test_event_payload_matches_go_json() -> None:
    from dex import CodecRegistry

    event = EmployeeChangedEvent(
        "140", action="Updated", last_changed=datetime.fromisoformat("2026-10-13T09:00:01Z")
    )
    encoded = CodecRegistry().resolve(EmployeeChangedEvent).encode(event).data
    assert json.loads(str(encoded)) == {
        "employeeId": "140",
        "action": "Updated",
        "lastChanged": "2026-10-13T09:00:01Z",
    }
