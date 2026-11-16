# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Port of the Go connector's recordModifiedPolled fetcher tests, over the same fake org."""

from collections.abc import AsyncIterator
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid

from dex_connectors.salesforce import (
    MAX_TRIGGER_FIELD_NAMES,
    RECORD_CHANGE_CREATED,
    RECORD_CHANGE_UPDATED,
    Client,
    Config,
    Credentials,
    RecordModifiedEvent,
    RecordModifiedTriggerConfiguration,
    validate_record_modified_trigger_configuration,
)
from dex_connectors.salesforce.record_modified_fetcher import RecordPollFetcher
from dex_connectors.salesforce.record_modified_trigger import string_field
from examples.record_changes.salesforce_org_fake import INSTANCE_URL, SalesforceOrgFake

POLL_TEST_TOKEN = "salesforce-poll-token"
CONNECTION = ConnectionRef("salesforce", "salesforce-crm")
CONTEXT = TriggerContext()
Events = list[TriggerEvent[RecordModifiedEvent]]


class ManualClock:
    """A manual clock shared by the client and the fake org."""

    def __init__(self) -> None:
        self.moment = datetime(2026, 3, 2, 9, 0, 0, tzinfo=UTC)

    def now(self) -> datetime:
        return self.moment

    def advance(self, duration: timedelta) -> None:
        self.moment += duration


@pytest.fixture
async def http_clients() -> AsyncIterator[list[httpx.AsyncClient]]:
    clients: list[httpx.AsyncClient] = []
    yield clients
    for client in clients:
        await client.aclose()


def new_poll_test_fetcher(
    clients: list[httpx.AsyncClient], page_size: int, configuration: RecordModifiedTriggerConfiguration
) -> tuple[RecordPollFetcher, SalesforceOrgFake, ManualClock]:
    clock = ManualClock()
    org = SalesforceOrgFake(POLL_TEST_TOKEN, clock.now)
    http_client = httpx.AsyncClient(transport=org.transport())
    clients.append(http_client)
    credentials = StaticCredentialProvider(
        {
            CONNECTION: Credentials(
                "salesforce-oauth", access_token=SecretString(POLL_TEST_TOKEN), instance_url=INSTANCE_URL
            )
        }
    )
    client = Client(Config(poll_page_size=page_size), credentials, http_client, now=clock.now)
    validate_record_modified_trigger_configuration(configuration)
    return RecordPollFetcher(client, CONNECTION, configuration), org, clock


async def fetch_all(fetcher: RecordPollFetcher, cursor: Cursor) -> tuple[Events, Cursor]:
    events: Events = []
    for _ in range(20):
        page = await fetcher.fetch_changes(CONTEXT, cursor)
        events.extend(page.changes)
        cursor = page.next_cursor
        if not page.has_more_pages:
            return events, cursor
    raise AssertionError("poll never caught up")


async def test_seed_records_existing_records_as_a_baseline_and_delivers_later_changes(
    http_clients: list[httpx.AsyncClient],
) -> None:
    configuration = RecordModifiedTriggerConfiguration("Opportunity", ("Name", "StageName", "Id"))
    fetcher, org, clock = new_poll_test_fetcher(http_clients, 200, configuration)
    existing = org.insert("Opportunity", {"Name": "Renewal", "StageName": "Prospecting"})
    org.insert("Account", {"Name": "Meridian"})
    cursor = await fetcher.seed_cursor(CONTEXT)
    assert existing in cursor.token, "the cursor sits on the newest existing record"

    events, cursor = await fetch_all(fetcher, cursor)
    assert events == [], "existing records are never delivered"

    created = org.insert("Opportunity", {"Name": "Expansion", "StageName": "Qualification"})
    org.update(existing, {"StageName": "Closed Won"})
    clock.advance(timedelta(seconds=1))
    refetched = await fetcher.fetch_changes(CONTEXT, cursor)
    events, next_cursor = await fetch_all(fetcher, cursor)
    assert len(events) == 2
    assert list(refetched.changes) == events, "a refetch from one cursor yields the same event IDs"
    assert events[0].payload.record_id == created
    assert events[0].payload.change_kind == RECORD_CHANGE_CREATED
    assert events[1].payload.record_id == existing
    assert events[1].payload.change_kind == RECORD_CHANGE_UPDATED
    assert string_field(events[1].payload.record, "StageName") == "Closed Won"
    assert events[0].id.startswith("sfdc-")
    assert events[1].payload.system_modstamp == events[1].occurred_at
    assert (
        "SELECT Id, CreatedDate, LastModifiedDate, SystemModstamp, Name, StageName FROM Opportunity"
        in org.requests[-1].soql
    )

    again, _ = await fetch_all(fetcher, next_cursor)
    assert again == []
    org.update(existing, {"StageName": "Closed Lost"})
    later, _ = await fetch_all(fetcher, next_cursor)
    assert len(later) == 1
    assert later[0].id != events[1].id, "a later change to one record gets a new event ID"


async def test_records_sharing_one_system_modstamp_page_by_id(http_clients: list[httpx.AsyncClient]) -> None:
    fetcher, org, _ = new_poll_test_fetcher(http_clients, 2, RecordModifiedTriggerConfiguration("Lead"))
    org.insert("Lead", {})
    cursor = await fetcher.seed_cursor(CONTEXT)
    ids = org.insert_batch("Lead", 5)
    events, _ = await fetch_all(fetcher, cursor)
    assert [event.payload.record_id for event in events] == ids


async def test_recent_changes_wait_for_the_settle_delay(http_clients: list[httpx.AsyncClient]) -> None:
    fetcher, org, clock = new_poll_test_fetcher(http_clients, 200, RecordModifiedTriggerConfiguration("Case"))
    cursor = await fetcher.seed_cursor(CONTEXT)
    assert '"systemModstamp":"2026-03-02T08:59:45Z"' in cursor.token, "an empty object seeds at the horizon"
    clock.advance(timedelta(seconds=50))
    org.insert("Case", {})  # Stamped 08:59:50, after the 08:59:35 horizon of a clock 50s back.
    clock.advance(timedelta(seconds=-50))
    queries = org.query_count
    page = await fetcher.fetch_changes(CONTEXT, cursor)
    assert list(page.changes) == []
    assert page.next_cursor == cursor
    assert org.query_count == queries, "a cursor at the horizon sends no request"
    clock.advance(timedelta(seconds=30))
    events, _ = await fetch_all(fetcher, cursor)
    assert len(events) == 1


async def test_salesforce_clock_behind_the_host_keeps_the_settle_delay(
    http_clients: list[httpx.AsyncClient],
) -> None:
    fetcher, org, clock = new_poll_test_fetcher(http_clients, 200, RecordModifiedTriggerConfiguration("Case"))
    org.clock_skew = timedelta(minutes=-1)  # Salesforce reads 08:59:00 while this host reads 09:00:00.
    cursor = await fetcher.seed_cursor(CONTEXT)
    assert '"systemModstamp":"2026-03-02T08:58:45Z"' in cursor.token
    recent = org.insert_recent("Case", {})  # Stamped 08:59:00, after Salesforce's horizon.
    events, cursor = await fetch_all(fetcher, cursor)
    assert events == [], "a change Salesforce stamped within the settle delay waits"
    clock.advance(timedelta(seconds=20))
    events, _ = await fetch_all(fetcher, cursor)
    assert [event.payload.record_id for event in events] == [recent]


async def test_salesforce_clock_behind_the_host_stops_a_page_at_its_horizon(
    http_clients: list[httpx.AsyncClient],
) -> None:
    fetcher, org, clock = new_poll_test_fetcher(http_clients, 200, RecordModifiedTriggerConfiguration("Case"))
    cursor = await fetcher.seed_cursor(CONTEXT)
    clock.advance(timedelta(minutes=1))
    settled = org.insert("Case", {})
    org.clock_skew = timedelta(seconds=-30)  # The host clock jumps 30s ahead of Salesforce between polls.
    org.insert_recent("Case", {})
    page = await fetcher.fetch_changes(CONTEXT, cursor)
    assert [event.payload.record_id for event in page.changes] == [settled]
    assert not page.has_more_pages
    events, _ = await fetch_all(fetcher, page.next_cursor)
    assert events == []
    clock.advance(timedelta(seconds=20))
    events, _ = await fetch_all(fetcher, page.next_cursor)
    assert len(events) == 1


@pytest.mark.parametrize(
    "token",
    [
        "not json",
        '{"sObjectType":"Contact","systemModstamp":"yesterday"}',
        '{"sObjectType":"Contact","systemModstamp":"2026-03-02T08:00:00Z","recordId":"bad id"}',
        '{"sObjectType":"Lead","systemModstamp":"2026-03-02T08:00:00Z"}',
    ],
)
async def test_invalid_cursors_are_marked_for_reseed(
    http_clients: list[httpx.AsyncClient], token: str
) -> None:
    fetcher, _, _ = new_poll_test_fetcher(http_clients, 200, RecordModifiedTriggerConfiguration("Contact"))
    with pytest.raises(Exception) as raised:
        await fetcher.fetch_changes(CONTEXT, Cursor(token))
    assert is_cursor_invalid(raised.value), token


async def test_request_limit_exceeded_pauses_the_poller(http_clients: list[httpx.AsyncClient]) -> None:
    fetcher, org, _ = new_poll_test_fetcher(http_clients, 200, RecordModifiedTriggerConfiguration("Contact"))
    org.rate_limit_next(2, "120")
    with pytest.raises(RateLimitedError) as raised:
        await fetcher.seed_cursor(CONTEXT)
    assert raised.value.retry_after == timedelta(minutes=2)
    assert "REQUEST_LIMIT_EXCEEDED" in str(raised.value)
    assert POLL_TEST_TOKEN not in str(raised.value)
    cursor = Cursor('{"sObjectType":"Contact","systemModstamp":"2026-03-02T08:00:00Z"}')
    with pytest.raises(RateLimitedError) as raised:
        await fetcher.fetch_changes(CONTEXT, cursor)
    assert not is_cursor_invalid(raised.value)


async def test_unreadable_object_is_a_poll_failure_not_a_reset(http_clients: list[httpx.AsyncClient]) -> None:
    fetcher, org, _ = new_poll_test_fetcher(http_clients, 200, RecordModifiedTriggerConfiguration("Contact"))
    org.rejected_objects["Contact"] = "INVALID_TYPE"
    with pytest.raises(Exception, match="INVALID_TYPE") as raised:
        await fetcher.seed_cursor(CONTEXT)
    assert not is_cursor_invalid(raised.value)
    cursor = Cursor('{"sObjectType":"Contact","systemModstamp":"2026-03-02T08:00:00Z"}')
    with pytest.raises(Exception) as raised:
        await fetcher.fetch_changes(CONTEXT, cursor)
    assert not is_cursor_invalid(raised.value)


def test_record_modified_trigger_configuration_validation() -> None:
    validate_record_modified_trigger_configuration(
        RecordModifiedTriggerConfiguration("Invoice__c", ("Amount__c",))
    )
    too_many = tuple("Field" + "x" * (index + 1) for index in range(MAX_TRIGGER_FIELD_NAMES + 1))
    for configuration in (
        RecordModifiedTriggerConfiguration(),
        RecordModifiedTriggerConfiguration("Bad Name"),
        RecordModifiedTriggerConfiguration("Contact", ("Account.Name",)),
        RecordModifiedTriggerConfiguration("Contact", ("Email", "email")),
        RecordModifiedTriggerConfiguration("Contact", too_many),
    ):
        with pytest.raises(ValueError):
            validate_record_modified_trigger_configuration(configuration)


def test_new_rejects_an_unsupported_poll_configuration() -> None:
    credentials: StaticCredentialProvider[Credentials] = StaticCredentialProvider({})
    with pytest.raises(ValueError, match="pollInterval"):
        Client(Config(poll_interval_seconds=1), credentials)
    with pytest.raises(ValueError, match="pollPageSize"):
        Client(Config(poll_page_size=501), credentials)
