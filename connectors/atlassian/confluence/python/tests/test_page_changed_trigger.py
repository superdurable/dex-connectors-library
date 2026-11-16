# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""pageChanged fetcher behavior against the fake site, configuration, and runner wiring."""

import asyncio
import json
from collections.abc import Awaitable, Callable
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
)

from dex_connectors.atlassian_confluence import (
    PAGE_CHANGE_CREATED,
    PAGE_CHANGE_UPDATED,
    PAGE_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Connection,
    Credentials,
    PageChangedEvent,
    PageChangedTriggerConfiguration,
    PageChangedTriggerRoute,
    PageTriggerRunnerConfig,
    decode_page_changed_event_payload,
    decode_page_changed_trigger_configuration,
    define_page_changed_trigger_binding,
    new_connection,
    new_page_changed_trigger,
    new_page_trigger_runner,
    validate_page_changed_trigger_configuration,
)
from dex_connectors.atlassian_confluence._page_change_fetcher import PageChangeFetcher

from .confluence_page_fake import ACCESS_TOKEN, CLOUD_ID, ConfluencePageSite

CONNECTION = ConnectionRef("atlassian", "confluence-trigger-test")


class Clock:
    """A settable clock shared by the client and the fake site."""

    def __init__(self) -> None:
        self.now = datetime(2026, 11, 2, 9, 0, 0, tzinfo=UTC)

    def read(self) -> datetime:
        return self.now

    def advance(self, duration: timedelta) -> None:
        self.now += duration


def new_client(site: ConfluencePageSite, clock: Clock | None = None, cloud_id: str = "") -> Client:
    credentials = Credentials(
        "client-id", SecretString("secret"), SecretString(ACCESS_TOKEN), SecretString("r")
    )
    return Client(
        Config(cloud_id, endpoint="http://127.0.0.1:12345"),
        StaticCredentialProvider({CONNECTION: credentials}),
        httpx.AsyncClient(transport=site.transport()),
        now=clock.read if clock else None,
    )


def new_fetcher(client: Client, space_key: str = "") -> PageChangeFetcher:
    return PageChangeFetcher(client, CONNECTION, PageChangedTriggerConfiguration(space_key=space_key))


async def test_baseline_is_skipped_and_each_new_version_arrives_once_oldest_first() -> None:
    clock = Clock()
    site = ConfluencePageSite(clock=clock.read)
    fetcher = new_fetcher(new_client(site, clock), "OPS")
    existing = site.publish("OPS", "Existing policy", "Before the Trigger started")
    context = TriggerContext()
    cursor = await fetcher.seed_cursor(context)
    assert cursor.snapshot is not None and [entry.key for entry in cursor.snapshot.entries] == [existing]
    page = await fetcher.fetch_changes(context, cursor)
    assert page.changes == () and not page.has_more_pages

    clock.advance(timedelta(seconds=30))
    created = site.publish("OPS", "Remote work policy", "Two days a week")
    clock.advance(timedelta(seconds=1))
    site.edit(existing, "Existing policy", "Revised")
    site.publish("HR", "Other space", "Ignored")
    page = await fetcher.fetch_changes(context, page.next_cursor)
    assert [(change.payload.page.id, change.payload.change_kind) for change in page.changes] == [
        (created, PAGE_CHANGE_CREATED),
        (existing, PAGE_CHANGE_UPDATED),
    ]
    assert page.changes[0].payload.cloud_id == CLOUD_ID, "the site comes from accessible resources"
    assert (
        page.changes[0].payload.page.web_url == f"https://ops.atlassian.net/wiki/spaces/OPS/pages/{created}"
    )
    again = await fetcher.fetch_changes(context, page.next_cursor)
    assert again.changes == (), "a delivered version is never delivered again"
    assert all('space = "OPS"' in cql for cql in site.cql_queries)


async def test_a_long_scan_spans_polls_and_an_expired_cursor_restarts_it() -> None:
    clock = Clock()
    site = ConfluencePageSite(clock=clock.read)
    fetcher = new_fetcher(new_client(site, clock, CLOUD_ID))
    context = TriggerContext()
    cursor = await fetcher.seed_cursor(context)
    for index in range(150):
        clock.advance(timedelta(milliseconds=10))
        site.publish("OPS", f"Page {index}", "body")
    page = await fetcher.fetch_changes(context, cursor)
    assert len(page.changes) == 100 and page.has_more_pages
    assert json.loads(page.next_cursor.token)["scan"]["searchCursor"]
    site.expire_search_cursors()
    restarted = await fetcher.fetch_changes(context, page.next_cursor)
    assert restarted.changes == () and restarted.has_more_pages
    assert "searchCursor" not in json.loads(restarted.next_cursor.token)["scan"]
    first = await fetcher.fetch_changes(context, restarted.next_cursor)
    assert first.changes == () and first.has_more_pages, "the first 100 were already delivered"
    rest = await fetcher.fetch_changes(context, first.next_cursor)
    assert len(rest.changes) == 50 and not rest.has_more_pages
    assert len({change.id for change in [*page.changes, *rest.changes]}) == 150


async def test_rate_limits_and_foreign_cursors() -> None:
    clock = Clock()
    site = ConfluencePageSite(clock=clock.read)
    client = new_client(site, clock, CLOUD_ID)
    context = TriggerContext()
    cursor = await new_fetcher(client, "OPS").seed_cursor(context)
    site.throttle_searches(1, 7)
    with pytest.raises(RateLimitedError) as limited:
        await new_fetcher(client, "OPS").fetch_changes(context, cursor)
    assert limited.value.retry_after == timedelta(seconds=7)
    with pytest.raises(Exception) as other_space:
        await new_fetcher(client, "HR").fetch_changes(context, cursor)
    assert is_cursor_invalid(other_space.value)
    other_site = new_client(ConfluencePageSite("aaaaaaaa-a1b2-4b33-8c44-def123456789"), clock, "")
    with pytest.raises(Exception) as foreign:
        await new_fetcher(other_site, "OPS").fetch_changes(context, cursor)
    assert is_cursor_invalid(foreign.value)


def test_configuration_validation_and_strict_decoding() -> None:
    validate_page_changed_trigger_configuration(PageChangedTriggerConfiguration())
    validate_page_changed_trigger_configuration(
        PageChangedTriggerConfiguration(space_key="~5b10ac8d82e05b22cc7d4ef5")
    )
    with pytest.raises(ValueError, match="space key"):
        validate_page_changed_trigger_configuration(PageChangedTriggerConfiguration(space_key=" OPS"))
    decoded = decode_page_changed_trigger_configuration(
        {"spaceId": "98306", "spaceKey": "OPS", "spaceName": "Ops"}
    )
    assert decoded == PageChangedTriggerConfiguration("98306", "OPS", "Ops")
    with pytest.raises(ValueError, match="unknown field"):
        decode_page_changed_trigger_configuration({"spaceKey": "OPS", "cql": "type = page"})
    with pytest.raises(ValueError):
        decode_page_changed_event_payload("{}", b"{}")
    with pytest.raises(ValueError, match="pollInterval"):
        Client(Config("", poll_interval_seconds=5), object())
    assert Client(Config("", poll_interval_seconds=0), object()).poll_interval_seconds == 60


def test_binding_factory_declares_the_trigger() -> None:
    binding = define_page_changed_trigger_binding(
        connection_name="confluence-page-watcher", binding_name="changes"
    )
    assert binding.definition == PAGE_CHANGED_TRIGGER_DEFINITION
    assert (binding.connection_name, binding.binding_name) == ("confluence-page-watcher", "changes")


class Recorder:
    def __init__(self) -> None:
        self.events: list[TriggerEvent[PageChangedEvent]] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[PageChangedEvent]) -> None:
        self.events.append(event)


async def wait_until(condition: Callable[[], Awaitable[bool]]) -> None:
    async with asyncio.timeout(10):
        while not await condition():
            await asyncio.sleep(0.01)


def new_trigger_connection(site: ConfluencePageSite) -> Connection:
    return new_connection(new_client(site, cloud_id=CLOUD_ID), CONNECTION)


async def test_runner_records_a_durable_baseline_then_delivers_through_the_inbox() -> None:
    site = ConfluencePageSite()
    connection = new_trigger_connection(site)
    objects, scope = InMemoryObjectStore(), Scope("confluence-test", "live")
    key = TriggerInboxKey("confluence", CONNECTION.name, "pageChanged", "changes")
    cursor = PollCursorStore(objects, scope, PollCursorKey.from_trigger_inbox_key(key))
    recorder = Recorder()
    target = new_durable_trigger_target(
        TriggerInbox(objects, scope, key), recorder, decode_payload=decode_page_changed_event_payload
    )
    site.publish("OPS", "Existing", "body")
    runner = new_page_trigger_runner(
        PageTriggerRunnerConfig(
            connection,
            [
                PageChangedTriggerRoute(
                    "changes", PageChangedTriggerConfiguration(space_key="OPS"), target, cursor
                )
            ],
        )
    )
    cancellation = TriggerCancellation()
    running = asyncio.create_task(runner.run(TriggerContext(cancellation)))

    async def is_seeded() -> bool:
        return (await cursor.read_poll_cursor()).state is not None

    async def is_delivered() -> bool:
        return bool(recorder.events)

    try:
        await wait_until(is_seeded)
        await asyncio.sleep(0.002)
        created = site.publish("OPS", "Created after the baseline", "body")
        runner.wake_poller("changes")
        await wait_until(is_delivered)
        assert [event.payload.page.id for event in recorder.events] == [created]
        state = (await cursor.read_poll_cursor()).state
        assert state is not None and json.loads(state.token)["spaceKey"] == "OPS"
        assert state.snapshot is not None and created in [entry.key for entry in state.snapshot.entries]
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running
        await connection.aclose()


async def test_single_binding_trigger_and_wiring_errors() -> None:
    site = ConfluencePageSite()
    connection = new_trigger_connection(site)
    recorder = Recorder()
    trigger = new_page_changed_trigger(
        connection=connection,
        binding_name="changes",
        configuration=PageChangedTriggerConfiguration(),
        target=recorder,
    )
    cancellation = TriggerCancellation()
    running = asyncio.create_task(trigger.run(TriggerContext(cancellation)))

    async def has_searched() -> bool:
        return site.search_requests() > 0

    await wait_until(has_searched)
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await asyncio.wait_for(running, 10)
    assert site.cql_queries[0].startswith("type = page AND lastmodified >= ")
    with pytest.raises(ValueError, match="routes are required"):
        new_page_trigger_runner(PageTriggerRunnerConfig(connection, []))
    with pytest.raises(ValueError, match="space key"):
        new_page_changed_trigger(
            connection=connection,
            binding_name="changes",
            configuration=PageChangedTriggerConfiguration(space_key="bad key"),
            target=recorder,
        )
    await connection.aclose()
