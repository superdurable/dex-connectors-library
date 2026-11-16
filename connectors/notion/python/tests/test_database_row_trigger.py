# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic tests of the databaseRowChanged fetcher, runner wiring, and Go time handling."""

import asyncio
import json
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest
import yaml
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import Cursor, PollRunner, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import Configuration, Scope

import dex_connectors.notion as package
from dex_connectors.notion import (
    DATABASE_ROW_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    DatabaseRowChangedEvent,
    DatabaseRowChangedTriggerConfiguration,
    DatabaseRowChangedTriggerRoute,
    DatabaseRowChangeKind,
    DatabaseRowTriggerRunnerConfig,
    ProjectDatabaseRowChangedTriggerRoute,
    ProjectDatabaseRowTriggerRunnerConfig,
    decode_database_row_changed_trigger_configuration,
    new_connection,
    new_database_row_changed_trigger,
    new_database_row_trigger_runner,
    new_project_database_row_trigger_runner,
    validate_database_row_changed_trigger_configuration,
)
from dex_connectors.notion._database_row_fetcher import DatabaseRowFetcher
from dex_connectors.notion._go_time import GoTime, parse_go_time
from tests.notion_rows_fake import ENDPOINT, TOKEN, NotionRowsFake

DATA_SOURCE_ID = "2a4d6f80-1b3c-4d5e-8f70-112233445566"
DATABASE_ID = "2a4d6f80-1b3c-4d5e-8f70-112233445500"
ROW_A = "2a4d6f80-1b3c-4d5e-8f70-0000000000a1"
ROW_B = "2a4d6f80-1b3c-4d5e-8f70-0000000000b2"
ROW_C = "2a4d6f80-1b3c-4d5e-8f70-0000000000c3"
REFERENCE = ConnectionRef("notion", "rows")
MANIFEST = yaml.safe_load((Path(__file__).parents[2] / "connector.yaml").read_text())
BASE_TIME = datetime(2026, 11, 2, 9, 0, tzinfo=UTC)


def minute(offset: int) -> datetime:
    return BASE_TIME + timedelta(minutes=offset)


def new_client(http_client: httpx.AsyncClient, **settings: Any) -> Client:
    credentials = StaticCredentialProvider({REFERENCE: Credentials(SecretString(TOKEN))})
    settings.setdefault("poll_interval_seconds", 5)
    return Client(Config(endpoint=ENDPOINT, **settings), credentials, http_client)


def new_fetcher(fake: NotionRowsFake, page_size: int = 100) -> DatabaseRowFetcher:
    return DatabaseRowFetcher(
        new_client(fake.http_client(), poll_page_size=page_size), REFERENCE, DATA_SOURCE_ID
    )


def context() -> TriggerContext:
    return TriggerContext(TriggerCancellation())


async def fetch_all(
    fetcher: DatabaseRowFetcher, cursor: Cursor
) -> tuple[list[TriggerEvent[DatabaseRowChangedEvent]], Cursor]:
    """Follow has_more_pages from cursor, as the kit does within one poll."""
    events: list[TriggerEvent[DatabaseRowChangedEvent]] = []
    for _ in range(20):
        page = await fetcher.fetch_changes(context(), cursor)
        events, cursor = events + list(page.changes), page.next_cursor
        if not page.has_more_pages:
            return events, cursor
    raise AssertionError("the fetcher never finished its query sequence")


def titles(events: list[TriggerEvent[DatabaseRowChangedEvent]]) -> list[str]:
    return [f"{event.payload.row.title}:{event.payload.change_kind}" for event in events]


class RecordingTarget:
    def __init__(self) -> None:
        self.events: list[TriggerEvent[DatabaseRowChangedEvent]] = []

    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[DatabaseRowChangedEvent]
    ) -> None:
        self.events.append(event)


async def test_baseline_delivers_no_existing_row_and_then_each_new_version_once() -> None:
    fake = NotionRowsFake(DATA_SOURCE_ID, DATABASE_ID)
    fake.create(ROW_A, "Existing old", minute(0))
    fake.create(ROW_B, "Existing newest", minute(10))
    fetcher = new_fetcher(fake)
    seed = await fetcher.seed_cursor(context())
    assert [request.body["sorts"][0]["direction"] for request in fake.requests if request.body] == [
        "descending",
        "ascending",
    ]
    assert fake.requests[1].body is not None
    assert fake.requests[1].body["filter"]["last_edited_time"]["on_or_after"] == "2026-11-02T09:08:00Z"
    events, cursor = await fetch_all(fetcher, seed)
    assert events == []

    fake.create(ROW_C, "New", minute(11))
    fake.edit(ROW_A, "Existing edited", minute(12))
    events, following = await fetch_all(fetcher, cursor)
    assert titles(events) == ["New:created", "Existing edited:updated"]
    assert events[0].payload.data_source_id == DATA_SOURCE_ID
    assert events[0].occurred_at == minute(11)
    refetched, _ = await fetch_all(fetcher, cursor)
    assert [event.id for event in refetched] == [event.id for event in events]
    again, _ = await fetch_all(fetcher, following)
    assert again == []


async def test_late_indexed_rows_inside_the_lookback_are_delivered_once() -> None:
    fake = NotionRowsFake(DATA_SOURCE_ID, DATABASE_ID)
    fetcher = new_fetcher(fake)
    seed = await fetcher.seed_cursor(context())
    fake.create(ROW_A, "A", minute(31))
    fake.create(ROW_B, "B late", minute(31))
    fake.hide_from_queries(ROW_B)
    events, cursor = await fetch_all(fetcher, seed)
    assert titles(events) == ["A:created"]
    fake.create(ROW_C, "C", minute(32))
    events, cursor = await fetch_all(fetcher, cursor)
    assert titles(events) == ["C:created"]
    fake.reveal_to_queries(ROW_B)
    events, _ = await fetch_all(fetcher, cursor)
    assert titles(events) == ["B late:created"]


async def test_a_burst_pages_through_and_a_stale_start_cursor_restarts_the_query() -> None:
    fake = NotionRowsFake(DATA_SOURCE_ID, DATABASE_ID)
    fetcher = new_fetcher(fake, page_size=1)
    seed = await fetcher.seed_cursor(context())
    for row_id, title in ((ROW_A, "A"), (ROW_B, "B"), (ROW_C, "C")):
        fake.create(row_id, title, minute(31))
    first = await fetcher.fetch_changes(context(), seed)
    assert first.has_more_pages and '"startCursor":"rows-1"' in first.next_cursor.token
    fake.expire_start_cursors()
    restarted = await fetcher.fetch_changes(context(), first.next_cursor)
    assert restarted.changes == () and restarted.has_more_pages
    assert "startCursor" not in restarted.next_cursor.token
    remaining, _ = await fetch_all(fetcher, restarted.next_cursor)
    assert titles(list(first.changes)) == ["A:created"]
    assert sorted(titles(remaining)) == ["B:created", "C:created"]


async def test_trashed_rows_are_not_delivered_and_failures_are_classified_for_the_kit() -> None:
    fake = NotionRowsFake(DATA_SOURCE_ID, DATABASE_ID)
    fake.create(ROW_A, "A", minute(0))
    fetcher = new_fetcher(fake)
    seed = await fetcher.seed_cursor(context())
    fake.trash(ROW_A, minute(31))
    events, _ = await fetch_all(fetcher, seed)
    assert events == []

    fake.rate_limit_next_requests(1, "7")
    with pytest.raises(RateLimitedError) as rate_limited:
        await fetcher.fetch_changes(context(), seed)
    assert rate_limited.value.retry_after == timedelta(seconds=7)

    for token in (
        "not json",
        '{"v":1,"dataSourceId":"2a4d6f80-1b3c-4d5e-8f70-999999999999","highWater":"2026-11-02T09:00:00Z"}',
        '{"v":9,"dataSourceId":"' + DATA_SOURCE_ID + '","highWater":"2026-11-02T09:00:00Z"}',
        '{"v":1,"dataSourceId":"' + DATA_SOURCE_ID + '"}',
    ):
        with pytest.raises(Exception) as invalid:
            await fetcher.fetch_changes(context(), Cursor(token))
        assert is_cursor_invalid(invalid.value), token

    unshared = DatabaseRowFetcher(
        new_client(fake.http_client()), REFERENCE, "2a4d6f80-1b3c-4d5e-8f70-999999999999"
    )
    with pytest.raises(Exception, match="share its database with the connection") as failed:
        await unshared.seed_cursor(context())
    assert not is_cursor_invalid(failed.value) and not isinstance(failed.value, RateLimitedError)
    assert fake.requests[-1].status == 404


async def test_runner_seeds_then_delivers_each_row_version_to_its_target() -> None:
    fake = NotionRowsFake(DATA_SOURCE_ID, DATABASE_ID)
    fake.create(ROW_A, "Existing", minute(0))
    connection = new_connection(new_client(fake.http_client()), REFERENCE)
    target = RecordingTarget()
    configuration = DatabaseRowChangedTriggerConfiguration(data_source_id=DATA_SOURCE_ID.replace("-", ""))
    runner = new_database_row_trigger_runner(
        DatabaseRowTriggerRunnerConfig(
            connection, [DatabaseRowChangedTriggerRoute("rows", configuration, target)]
        )
    )
    cancellation = TriggerCancellation()
    running = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        await eventually(lambda: len(fake.requests) >= 2)
        fake.create(ROW_B, "New", minute(1))
        runner.wake_poller("rows")
        await eventually(lambda: len(target.events) == 1)
        assert target.events[0].payload.change_kind == DatabaseRowChangeKind.CREATED
        assert target.events[0].payload.data_source_id == DATA_SOURCE_ID
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running


async def eventually(condition_met: Any) -> None:
    async with asyncio.timeout(10):
        while not condition_met():
            await asyncio.sleep(0.01)


def test_configuration_and_poll_settings_are_validated() -> None:
    validate_database_row_changed_trigger_configuration(
        DatabaseRowChangedTriggerConfiguration(
            data_source_id="https://www.notion.so/2a4d6f801b3c4d5e8f70112233445566"
        )
    )
    with pytest.raises(ValueError, match="dataSourceId is required"):
        validate_database_row_changed_trigger_configuration(DatabaseRowChangedTriggerConfiguration())
    with pytest.raises(ValueError):
        validate_database_row_changed_trigger_configuration(
            DatabaseRowChangedTriggerConfiguration(data_source_id="not-an-id")
        )
    with pytest.raises(ValueError, match="unknown field"):
        decode_database_row_changed_trigger_configuration({"dataSourceId": DATA_SOURCE_ID, "databaseId": "x"})
    with pytest.raises(ValueError, match="pollInterval must be at least 5s"):
        new_client(httpx.AsyncClient(), poll_interval_seconds=1)
    with pytest.raises(ValueError, match="pollPageSize must be between 1 and 100"):
        new_client(httpx.AsyncClient(), poll_page_size=101)


def test_runner_rejects_incomplete_routes_and_mock_connections() -> None:
    connection = new_connection(new_client(httpx.AsyncClient()), REFERENCE)
    target = RecordingTarget()
    configuration = DatabaseRowChangedTriggerConfiguration(data_source_id=DATA_SOURCE_ID)
    for routes in (
        [],
        [DatabaseRowChangedTriggerRoute("", configuration, target)],
        [DatabaseRowChangedTriggerRoute("rows", configuration, None)],  # type: ignore[arg-type]
        [DatabaseRowChangedTriggerRoute("rows", DatabaseRowChangedTriggerConfiguration(), target)],
    ):
        with pytest.raises(ValueError):
            new_database_row_trigger_runner(DatabaseRowTriggerRunnerConfig(connection, routes))
    with pytest.raises(ValueError, match="dataSourceId"):
        new_database_row_changed_trigger(
            connection=connection,
            binding_name="rows",
            configuration=DatabaseRowChangedTriggerConfiguration(),
            target=target,
        )
    from dex_connectors.notion.notionmock import Mock

    with pytest.raises(ValueError, match="new_connection or new_project_connection"):
        new_database_row_trigger_runner(
            DatabaseRowTriggerRunnerConfig(
                Mock("rows").connection(), [DatabaseRowChangedTriggerRoute("rows", configuration, target)]
            )
        )
    assert isinstance(
        new_database_row_changed_trigger(
            connection=connection, binding_name="rows", configuration=configuration, target=target
        ),
        object,
    )
    assert isinstance(
        new_database_row_trigger_runner(
            DatabaseRowTriggerRunnerConfig(
                connection, [DatabaseRowChangedTriggerRoute("rows", configuration, target)]
            )
        ),
        PollRunner,
    )


def test_project_runner_requires_a_saved_data_source() -> None:
    connection = new_connection(new_client(httpx.AsyncClient()), REFERENCE)

    class Project:
        configuration = Configuration(Scope("notion-trigger-test", "live"), 1, ())

    config = ProjectDatabaseRowTriggerRunnerConfig(
        [ProjectDatabaseRowChangedTriggerRoute("rows", RecordingTarget())]
    )
    with pytest.raises(ValueError, match="has no saved data source"):
        new_project_database_row_trigger_runner(Project(), connection, config)  # type: ignore[arg-type]


@pytest.mark.parametrize(
    ("text", "unix_nanoseconds", "rendered"),
    [
        ("2026-11-02T09:00:00.000Z", 1793610000 * 10**9, "2026-11-02T09:00:00Z"),
        ("2026-11-02T10:00:00.5+01:00", 1793610000 * 10**9 + 5 * 10**8, "2026-11-02T10:00:00.5+01:00"),
        ("2026-11-02T9:00:00,1234567891Z", 1793610000 * 10**9 + 123456789, "2026-11-02T09:00:00.123456789Z"),
        ("0000-02-29T00:00:00-00:00", -62162121600 * 10**9, "0000-02-29T00:00:00Z"),
        ("1970-01-01T00:00:00Z", 0, "1970-01-01T00:00:00Z"),
    ],
)
def test_go_times_parse_and_format_as_go_does(text: str, unix_nanoseconds: int, rendered: str) -> None:
    parsed = parse_go_time(text)
    assert parsed.unix_nanoseconds == unix_nanoseconds
    assert parsed.format_rfc3339_nano() == rendered


@pytest.mark.parametrize(
    "text",
    ["2026-02-29T00:00:00Z", "2026-11-02T24:00:00Z", "2026-11-02T09:00:00", "2026-11-02 09:00:00Z", "x"],
)
def test_go_times_refuse_what_go_refuses(text: str) -> None:
    with pytest.raises(ValueError):
        parse_go_time(text)
    with pytest.raises(ValueError):
        GoTime(parse_go_time("9999-12-31T23:59:59Z").unix_nanoseconds + 10**9).format_rfc3339_nano()


def test_python_trigger_matches_manifest_and_descriptor() -> None:
    assert MANIFEST["spec"]["languages"]["python"]["triggers"] == ["databaseRowChanged"]
    (manifest_trigger,) = MANIFEST["spec"]["triggers"]
    assert DATABASE_ROW_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name == manifest_trigger["name"]
    assert DATABASE_ROW_CHANGED_TRIGGER_DEFINITION.description == manifest_trigger["description"]
    descriptor: dict[str, Any] = json.loads(
        Path(package.__file__).with_name("dex-connector.json").read_text()
    )
    (factory,) = [factory for factory in descriptor["factories"] if factory["kind"] == "trigger"]
    assert factory["name"] == "define_database_row_changed_trigger_binding"
    assert factory["triggerName"] == "databaseRowChanged"
    assert callable(getattr(package, factory["name"]))
