# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the crmObjectChanged example through a real Dex Worker and Client against the fake HubSpot search."""

import asyncio
import os
from collections.abc import AsyncIterator, Awaitable, Callable, Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any

import dex
import httpx
import pytest
from dex_connectors.sdk import TriggerCancellation, TriggerLogLevel, TriggerLogValue
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConfigurationStore,
    ConnectionConfiguration,
    ConnectionKey,
    ConnectionStore,
    CredentialMaterial,
    InMemoryObjectStore,
    LoadedProject,
    PollCursorKey,
    Scope,
    SnapshotRef,
)

from dex_connectors.hubspot import ProjectCRMObjectTriggerRunner

from .flow import CONNECTION_NAME, CRM_OBJECT_CHANGED_TRIGGER_BINDING, ChangeOutcome
from .hubspot_search_fake import ACCESS_TOKEN, FailedResponse, HubSpotSearchFake
from .watcher import run_crm_object_change_watcher

SCOPE = Scope("hubspot-crm-object-changed", "live")
CURSOR_KEY = PollCursorKey("hubspot", CONNECTION_NAME, "crmObjectChanged", CRM_OBJECT_CHANGED_TRIGGER_BINDING)


class RecordingLogger:
    """Keeps every Trigger record, so the test counts Flow-start deliveries."""

    def __init__(self) -> None:
        self.records: list[tuple[TriggerLogLevel, str, dict[str, TriggerLogValue]]] = []

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        self.records.append((level, message, dict(attributes)))

    def delivered_flow_ids(self) -> list[str]:
        """Return each started Flow ID once, in delivery order."""
        flow_ids = [
            str(attrs["flow_id"])
            for _, message, attrs in self.records
            if message == "trigger event delivered"
        ]
        return list(dict.fromkeys(flow_ids))


@dataclass
class Environment:
    """One project over storage that survives a process restart, with a deals binding."""

    fake: HubSpotSearchFake
    objects: InMemoryObjectStore = field(default_factory=InMemoryObjectStore)
    logger: RecordingLogger = field(default_factory=RecordingLogger)

    def project(self) -> LoadedProject:
        configuration = Configuration(
            SCOPE,
            1,
            (
                ConnectionConfiguration(
                    "hubspot",
                    CONNECTION_NAME,
                    "github.com/superdurable/dex-connectors-library/connectors/hubspot/go",
                    "hubspot",
                    {"pollInterval": 5_000_000_000},
                    "private-app-token",
                ),
            ),
            (
                {
                    "connectorId": "hubspot",
                    "connectionName": CONNECTION_NAME,
                    "triggerName": "crmObjectChanged",
                    "bindingName": CRM_OBJECT_CHANGED_TRIGGER_BINDING,
                    "configuration": {"objectType": "deals"},
                },
            ),
        )
        return LoadedProject(
            configuration,
            SnapshotRef("k", "1", "sha256:" + "0" * 64),
            ConnectionStore(self.objects, SCOPE),
            ConfigurationStore(self.objects, SCOPE),
        )

    async def store_credential(self) -> None:
        """Save the private app token as Dex Web does, once for every later process."""
        material = CredentialMaterial(
            {"auth_method": "private-app-token", "access_token": ACCESS_TOKEN}, None, "private-app-token"
        )
        await ConnectionStore(self.objects, SCOPE).replace_credential(
            ConnectionKey("hubspot", CONNECTION_NAME), 0, material
        )

    async def is_seeded(self) -> bool:
        record = await self.project().poll_cursor_store(CURSOR_KEY).read_poll_cursor()
        return record.state is not None


@dataclass
class Process:
    """One run of the application; stopping it models a process exit."""

    client: dex.AsyncClient
    runner: ProjectCRMObjectTriggerRunner
    cancellation: TriggerCancellation
    task: "asyncio.Task[None]"

    async def stop(self) -> None:
        self.cancellation.cancel()
        await self.task


@pytest.fixture
async def http_client() -> AsyncIterator[list[httpx.AsyncClient]]:
    clients: list[httpx.AsyncClient] = []
    yield clients
    for client in clients:
        await client.aclose()


async def new_environment() -> Environment:
    # A start time unique to the run keeps Trigger event IDs, and so Flow IDs, unique on a shared Dex.
    environment = Environment(HubSpotSearchFake("deals", datetime.now(UTC)))
    await environment.store_credential()
    return environment


async def start_process(environment: Environment, clients: list[httpx.AsyncClient]) -> Process:
    http_client = httpx.AsyncClient(transport=environment.fake.transport())
    clients.append(http_client)
    cancellation = TriggerCancellation()
    ready: asyncio.Future[tuple[dex.AsyncClient, ProjectCRMObjectTriggerRunner]] = (
        asyncio.get_running_loop().create_future()
    )
    task = asyncio.create_task(
        run_crm_object_change_watcher(
            environment.project(),
            os.environ["DEX_FLOW_SERVICE_ADDRESS"],
            cancellation,
            http_client=http_client,
            logger=environment.logger,
            on_ready=lambda client, runner: ready.set_result((client, runner)),
        )
    )
    started: set[asyncio.Future[Any]] = {task, ready}
    done, _ = await asyncio.wait(started, timeout=60, return_when=asyncio.FIRST_COMPLETED)
    if ready not in done:
        cancellation.cancel()
        await task
        raise AssertionError("the watcher stopped before it was ready")
    client, runner = ready.result()
    return Process(client, runner, cancellation, task)


async def eventually(condition: Callable[[], Awaitable[bool] | bool], timeout: float = 30) -> None:
    async with asyncio.timeout(timeout):
        while True:
            result = condition()
            if await result if not isinstance(result, bool) else result:
                return
            await asyncio.sleep(0.05)


async def wait_for_outcomes(environment: Environment, client: dex.AsyncClient, count: int) -> dict[str, Any]:
    """Wait for count delivered changes, then each change's Flow, keyed by record ID."""
    await eventually(lambda: len(environment.logger.delivered_flow_ids()) >= count)
    outcomes: dict[str, ChangeOutcome] = {}
    for flow_id in environment.logger.delivered_flow_ids():
        result = await client.wait_for_flow(flow_id, timedelta(seconds=60))
        assert result.status == dex.FlowStatus.COMPLETED, f"{flow_id}: {result.error_message}"
        outcome = result.single_output(ChangeOutcome)
        outcomes[outcome.object_id] = outcome
    return outcomes


async def test_changes_start_one_flow_each_across_a_restart(http_client: list[httpx.AsyncClient]) -> None:
    """Existing records are a baseline, each change starts one Flow, and changes while stopped arrive."""
    environment = await new_environment()
    renewal = environment.fake.create({"dealname": "Renewal", "dealstage": "appointmentscheduled"})
    environment.fake.create({"dealname": "Untouched"})
    first = await start_process(environment, http_client)
    try:
        await eventually(environment.is_seeded)
        expansion = environment.fake.create({"dealname": "Expansion", "dealstage": "appointmentscheduled"})
        environment.fake.update(renewal, {"dealstage": "qualifiedtobuy"})
        first.runner.wake_poller(CRM_OBJECT_CHANGED_TRIGGER_BINDING)
        outcomes = await wait_for_outcomes(environment, first.client, 2)
        assert outcomes[expansion].change_kind == "created"
        assert outcomes[expansion].current.properties["dealname"] == "Expansion"
        assert outcomes[renewal].change_kind == "updated"
        assert outcomes[renewal].current.properties["dealstage"] == "qualifiedtobuy", "read back from HubSpot"
    finally:
        await first.stop()

    offline = environment.fake.create({"dealname": "Created while stopped"})
    environment.fake.update(expansion, {"dealstage": "closedwon"})
    second = await start_process(environment, http_client)
    try:
        second.runner.wake_poller(CRM_OBJECT_CHANGED_TRIGGER_BINDING)
        outcomes = await wait_for_outcomes(environment, second.client, 4)
        assert outcomes[offline].current.properties["dealname"] == "Created while stopped"
        assert outcomes[expansion].change_kind == "updated"
        assert outcomes[expansion].current.properties["dealstage"] == "closedwon"
        searches = environment.fake.search_request_count
        second.runner.wake_poller(CRM_OBJECT_CHANGED_TRIGGER_BINDING)
        await eventually(lambda: environment.fake.search_request_count > searches)
        assert len(environment.logger.delivered_flow_ids()) == 4, "no change twice and never the baseline"
    finally:
        await second.stop()


async def test_rate_limited_search_pauses_then_delivers(http_client: list[httpx.AsyncClient]) -> None:
    """A 429 with Retry-After pauses polling, and the change arrives once the pause ends."""
    environment = await new_environment()
    environment.fake.create({"dealname": "Baseline"})
    process = await start_process(environment, http_client)
    try:
        await eventually(environment.is_seeded)
        environment.fake.fail_next_searches(
            FailedResponse(429, '{"category":"RATE_LIMITS","policyName":"SECONDLY"}', {"Retry-After": "1"})
        )
        limited = environment.fake.search_request_count
        created = environment.fake.create({"dealname": "After the pause"})
        process.runner.wake_poller(CRM_OBJECT_CHANGED_TRIGGER_BINDING)
        await eventually(lambda: environment.fake.search_request_count > limited)
        outcomes = await wait_for_outcomes(environment, process.client, 1)
        assert outcomes[created].current.properties["dealname"] == "After the pause"
        assert environment.fake.search_request_count > limited + 1, "the poll after the pause searched again"
        assert any(
            message == "poll rate limited; pausing connection" for _, message, _ in environment.logger.records
        )
    finally:
        await process.stop()
