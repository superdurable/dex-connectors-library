# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Port of the Go connector's crmObjectChanged fetcher tests, over the same fake HubSpot search."""

import json
from collections.abc import AsyncIterator
from datetime import UTC, datetime, timedelta
from pathlib import Path

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

from dex_connectors.hubspot import (
    MAXIMUM_TRIGGER_PROPERTIES,
    Client,
    Config,
    Credentials,
    CRMObjectChangedEvent,
    CRMObjectChangedTriggerConfiguration,
    decode_crm_object_changed_event_payload,
    validate_crm_object_changed_trigger_configuration,
)
from dex_connectors.hubspot.crm_object_change_cursor import (
    MAXIMUM_SEEN_RECORD_VERSIONS,
    CRMObjectChangeCursor,
    decode_crm_object_change_cursor,
    encode_crm_object_change_cursor,
    lag_window_start,
    prune_seen,
)
from dex_connectors.hubspot.crm_object_change_fetcher import (
    CRMObjectChangeFetcher,
    crm_object_change_id,
    parse_rfc3339_milliseconds,
)
from examples.crm_object_changed.hubspot_search_fake import ACCESS_TOKEN, FailedResponse, HubSpotSearchFake

FAKE_START = datetime(2026, 10, 25, 10, 0, 0, tzinfo=UTC)
CONNECTION = ConnectionRef("hubspot", "hubspot-crm-watcher")
CONTEXT = TriggerContext()
Events = list[TriggerEvent[CRMObjectChangedEvent]]


@pytest.fixture
async def http_clients() -> AsyncIterator[list[httpx.AsyncClient]]:
    clients: list[httpx.AsyncClient] = []
    yield clients
    for client in clients:
        await client.aclose()


def new_fake_change_fetcher(
    clients: list[httpx.AsyncClient], object_type: str, *properties: str
) -> tuple[CRMObjectChangeFetcher, HubSpotSearchFake]:
    fake = HubSpotSearchFake(object_type, FAKE_START)
    http_client = httpx.AsyncClient(transport=fake.transport())
    clients.append(http_client)
    credentials = StaticCredentialProvider(
        {CONNECTION: Credentials("private-app-token", SecretString(ACCESS_TOKEN))}
    )
    configuration = CRMObjectChangedTriggerConfiguration(object_type, properties)
    validate_crm_object_changed_trigger_configuration(configuration)
    return CRMObjectChangeFetcher(Client(Config(), credentials, http_client), CONNECTION, configuration), fake


async def drain_changes(fetcher: CRMObjectChangeFetcher, cursor: Cursor) -> tuple[Events, Cursor]:
    """Fetch until the feed has no more pages, as one kit poll does."""
    events: Events = []
    for _ in range(50):
        page = await fetcher.fetch_changes(CONTEXT, cursor)
        events.extend(page.changes)
        cursor = page.next_cursor
        if not page.has_more_pages:
            return events, cursor
    raise AssertionError("change feed did not finish within 50 pages")


async def test_seed_records_a_baseline_and_later_changes_arrive_once_with_their_kind(
    http_clients: list[httpx.AsyncClient],
) -> None:
    fetcher, fake = new_fake_change_fetcher(http_clients, "deals", "dealname", "dealstage")
    existing = fake.create({"dealname": "Existing renewal", "dealstage": "appointmentscheduled"})
    events, cursor = await drain_changes(fetcher, await fetcher.seed_cursor(CONTEXT))
    assert events == [], "records that exist at seed are the baseline"
    created = fake.create({"dealname": "New expansion", "dealstage": "appointmentscheduled"})
    fake.update(existing, {"dealstage": "qualifiedtobuy"})
    before = cursor
    events, cursor = await drain_changes(fetcher, cursor)
    assert [(event.payload.object.id, event.payload.change_kind) for event in events] == [
        (created, "created"),
        (existing, "updated"),
    ]
    assert events[1].payload.object.properties["dealstage"] == "qualifiedtobuy"
    assert events[1].occurred_at == events[1].payload.modified_at
    refetched, _ = await drain_changes(fetcher, before)
    assert [event.id for event in refetched] == [event.id for event in events]
    again, _ = await drain_changes(fetcher, cursor)
    assert again == [], "the lag-window re-read skips delivered versions"


async def test_search_uses_the_last_modified_property_and_requested_properties(
    http_clients: list[httpx.AsyncClient],
) -> None:
    fetcher, fake = new_fake_change_fetcher(http_clients, "contacts", "email")
    fake.create({"email": "ada@example.com"})
    await drain_changes(fetcher, await fetcher.seed_cursor(CONTEXT))
    bodies = fake.search_bodies
    assert bodies[0]["sorts"] == [{"propertyName": "lastmodifieddate", "direction": "DESCENDING"}]
    assert bodies[1]["sorts"] == [{"propertyName": "lastmodifieddate", "direction": "ASCENDING"}]
    assert bodies[1]["properties"] == ["email", "createdate", "lastmodifieddate"]
    assert bodies[1]["filterGroups"][0]["filters"][0]["operator"] == "GTE"  # type: ignore[index]
    assert bodies[1]["limit"] == 100


async def test_empty_object_type_seeds_at_zero_and_delivers_the_first_record(
    http_clients: list[httpx.AsyncClient],
) -> None:
    fetcher, fake = new_fake_change_fetcher(http_clients, "companies")
    cursor = await fetcher.seed_cursor(CONTEXT)
    created = fake.create({"name": "Analytical Engines"})
    events, _ = await drain_changes(fetcher, cursor)
    assert [(event.payload.object.id, event.payload.change_kind) for event in events] == [
        (created, "created")
    ]


async def test_record_indexed_late_within_the_lag_window_is_still_delivered(
    http_clients: list[httpx.AsyncClient],
) -> None:
    fetcher, fake = new_fake_change_fetcher(http_clients, "deals")
    fake.create({"dealname": "Baseline"})
    cursor = await fetcher.seed_cursor(CONTEXT)
    late = fake.create_unindexed({"dealname": "Indexed late"})
    prompt = fake.create({"dealname": "Indexed at once"})
    events, cursor = await drain_changes(fetcher, cursor)
    assert [event.payload.object.id for event in events] == [prompt]
    fake.index(late)
    events, _ = await drain_changes(fetcher, cursor)
    assert [(event.payload.object.id, event.payload.change_kind) for event in events] == [(late, "updated")]


async def test_more_records_than_a_page_at_one_time_page_by_offset_and_arrive_once(
    http_clients: list[httpx.AsyncClient],
) -> None:
    fetcher, fake = new_fake_change_fetcher(http_clients, "contacts")
    cursor = await fetcher.seed_cursor(CONTEXT)
    imported = fake.create_at_same_time(250, {"lifecyclestage": "lead"})
    after = fake.create({"lifecyclestage": "customer"})
    events, cursor = await drain_changes(fetcher, cursor)
    identifiers = [event.payload.object.id for event in events]
    assert len(identifiers) == len(set(identifiers))
    assert set(identifiers) == {*imported, after}
    assert (await drain_changes(fetcher, cursor))[0] == []


async def test_fetch_maps_rate_limits_quotas_and_rejected_offsets_to_kit_signals(
    http_clients: list[httpx.AsyncClient],
) -> None:
    fetcher, fake = new_fake_change_fetcher(http_clients, "deals")
    cursor = await fetcher.seed_cursor(CONTEXT)
    fake.fail_next_searches(
        FailedResponse(429, '{"category":"RATE_LIMITS","policyName":"SECONDLY"}', {"Retry-After": "7"})
    )
    with pytest.raises(RateLimitedError) as limited:
        await fetcher.fetch_changes(CONTEXT, cursor)
    assert limited.value.retry_after == timedelta(seconds=7)
    fake.fail_next_searches(FailedResponse(429, '{"category":"RATE_LIMITS","policyName":"DAILY"}'))
    with pytest.raises(RateLimitedError) as quota:
        await fetcher.fetch_changes(CONTEXT, cursor)
    assert quota.value.retry_after == timedelta(0)
    fake.fail_next_searches(FailedResponse(502, "{}"))
    with pytest.raises(Exception) as unavailable:
        await fetcher.fetch_changes(CONTEXT, cursor)
    assert not is_cursor_invalid(unavailable.value) and not isinstance(unavailable.value, RateLimitedError)
    paging = decode_crm_object_change_cursor(cursor.token)
    paging.after = "9900"
    fake.fail_next_searches(FailedResponse(400, '{"category":"VALIDATION_ERROR"}'))
    with pytest.raises(Exception) as rejected_offset:
        await fetcher.fetch_changes(CONTEXT, encode_crm_object_change_cursor(paging))
    assert is_cursor_invalid(rejected_offset.value), "a rejected paging offset reseeds"
    fake.fail_next_searches(FailedResponse(400, '{"category":"VALIDATION_ERROR"}'))
    with pytest.raises(Exception) as rejected:
        await fetcher.fetch_changes(CONTEXT, cursor)
    assert not is_cursor_invalid(rejected.value), "a rejected search without an offset is a failed poll"


@pytest.mark.parametrize(
    "token",
    [
        "not json",
        '{"objectType":"contacts","floorMs":1,"highWaterMs":1,"anchorMs":2}',
        '{"objectType":"deals","floorMs":5,"highWaterMs":4,"anchorMs":6}',
        '{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"after":"x"}',
        '{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"seen":{"abc":3}}',
        '{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"droppedThroughMs":5}',
        '{"objectType":"deals","floorMs":1.0,"highWaterMs":1,"anchorMs":2}',
        '{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"seen":[]}',
        '{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"seen":{"7":1e0}}',
    ],
)
async def test_unusable_cursors_are_marked_invalid(http_clients: list[httpx.AsyncClient], token: str) -> None:
    fetcher, _ = new_fake_change_fetcher(http_clients, "deals")
    with pytest.raises(Exception) as unusable:
        await fetcher.fetch_changes(CONTEXT, Cursor(token))
    assert is_cursor_invalid(unusable.value)


def test_last_modified_dates_parse_with_go_rfc3339_nano_rules() -> None:
    assert parse_rfc3339_milliseconds("2026-01-02T03:04:05.1234567891239Z") == 1767323045123
    assert parse_rfc3339_milliseconds("2026-01-02T03:04:05,5Z") == 1767323045500
    assert parse_rfc3339_milliseconds("2026-01-02T03:04:05+05:60") == 1767301445000
    assert parse_rfc3339_milliseconds("2026-01-02T03:04:05.999+24:00") == 1767236645999
    for text in (
        "2026-01-02T03:04:05+05:61",
        "2026-01-02T03:04:05+25:00",
        "2026-02-29T03:04:05Z",
        "2026-01-02T03:04:05.Z",
        "2026-01-02T03:04:05z",
        "\u0662026-01-02T03:04:05Z",
    ):
        assert parse_rfc3339_milliseconds(text) is None, text


def test_prune_seen_keeps_the_newest_versions_inside_the_lag_window() -> None:
    position = CRMObjectChangeCursor("deals", 0, 10_000_000, 1, seen={"1": 1})
    for index in range(MAXIMUM_SEEN_RECORD_VERSIONS + 10):
        position.seen[str(100_000 + index)] = 10_000_000 - index
    prune_seen(position)
    assert len(position.seen) == MAXIMUM_SEEN_RECORD_VERSIONS
    assert "1" not in position.seen and "100000" in position.seen
    assert str(100_000 + MAXIMUM_SEEN_RECORD_VERSIONS) not in position.seen, "the oldest versions go first"
    assert position.dropped_through_ms == 10_000_000 - MAXIMUM_SEEN_RECORD_VERSIONS
    assert lag_window_start(position) == position.dropped_through_ms + 1
    assert len(encode_crm_object_change_cursor(position).token.encode()) < 64 << 10


def test_trigger_configuration_validation() -> None:
    with pytest.raises(ValueError, match="objectType is required"):
        validate_crm_object_changed_trigger_configuration(CRMObjectChangedTriggerConfiguration(""))
    for invalid in (
        CRMObjectChangedTriggerConfiguration("tickets"),
        CRMObjectChangedTriggerConfiguration("deals", ("bad name",)),
        CRMObjectChangedTriggerConfiguration(
            "deals", tuple(f"property_{index}" for index in range(MAXIMUM_TRIGGER_PROPERTIES + 1))
        ),
    ):
        with pytest.raises(ValueError):
            validate_crm_object_changed_trigger_configuration(invalid)
    validate_crm_object_changed_trigger_configuration(
        CRMObjectChangedTriggerConfiguration("deals", ("dealname", "createdate"))
    )


async def test_burst_larger_than_the_seen_capacity_is_not_redelivered_on_later_polls(
    http_clients: list[httpx.AsyncClient],
) -> None:
    fetcher, fake = new_fake_change_fetcher(http_clients, "contacts")
    cursor = await fetcher.seed_cursor(CONTEXT)
    imported = fake.create_at_same_time(MAXIMUM_SEEN_RECORD_VERSIONS + 250, {"lifecyclestage": "lead"})
    events, cursor = await drain_changes(fetcher, cursor)
    assert len(events) == len(imported)
    for _ in range(3):
        events, cursor = await drain_changes(fetcher, cursor)
        assert events == [], "an import larger than the seen set must not repeat on every poll"
    later = fake.create({"lifecyclestage": "customer"})
    events, _ = await drain_changes(fetcher, cursor)
    assert [event.payload.object.id for event in events] == [later]


async def test_cursor_documents_and_event_ids_match_the_go_connector_byte_for_byte(
    http_clients: list[httpx.AsyncClient],
) -> None:
    """Replay the scenario recorded from the Go fetcher over its hubspotsearchfake."""
    fixture = json.loads((Path(__file__).parent / "fixtures/go-crm-object-change-cursors.json").read_text())
    fetcher, fake = new_fake_change_fetcher(http_clients, "deals", "dealname")
    existing = fake.create({"dealname": "Existing"})
    cursor = await fetcher.seed_cursor(CONTEXT)
    assert cursor.token == fixture["seed"]
    pages: list[tuple[str, bool, list[str], list[TriggerEvent[CRMObjectChangedEvent]]]] = []

    async def poll() -> None:
        nonlocal cursor
        for _ in range(20):
            page = await fetcher.fetch_changes(CONTEXT, cursor)
            changes = list(page.changes)
            pages.append(
                (page.next_cursor.token, page.has_more_pages, [event.id for event in changes], changes)
            )
            cursor = page.next_cursor
            if not page.has_more_pages:
                return

    await poll()
    fake.create({"dealname": "New"})
    fake.update(existing, {"dealname": "Renamed"})
    await poll()
    fake.create_at_same_time(150, {"dealname": "Imported"})
    await poll()
    await poll()
    expected = [(page["token"], page["hasMorePages"], page["eventIds"]) for page in fixture["pages"]]
    assert [page[:3] for page in pages] == expected
    for (_, _, _, changes), recorded in zip(pages, fixture["pages"], strict=True):
        for change, payload in zip(changes, recorded["payloads"], strict=False):
            decoded = decode_crm_object_changed_event_payload(
                json.dumps(payload), json.dumps(payload).encode()
            )
            assert change.payload == decoded, "a Go-recorded payload decodes to the same event"
    position = CRMObjectChangeCursor(
        "deals", 0, 10_000_000, 7, "200", {"1": 1, "999": 9_999_999, "1000": 9_999_999}
    )
    for index in range(MAXIMUM_SEEN_RECORD_VERSIONS + 10):
        position.seen[str(100_000 + index)] = 10_000_000 - index
    prune_seen(position)
    assert encode_crm_object_change_cursor(position).token == fixture["pruned"]
    assert (
        encode_crm_object_change_cursor(decode_crm_object_change_cursor(fixture["pruned"])).token
        == fixture["pruned"]
    )
    assert crm_object_change_id("contacts", "12345", 1761386401123) == fixture["eventId"]
