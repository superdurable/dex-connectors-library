# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the recordModifiedPolled example through a real Dex Worker and Client against the fake org."""

import asyncio
import os
import time
from collections.abc import AsyncIterator, Awaitable, Callable, Mapping
from dataclasses import dataclass, field
from datetime import timedelta
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

from dex_connectors.salesforce import ProjectRecordTriggerRunner

from .flow import CONNECTION_NAME, RECORD_MODIFIED_TRIGGER_BINDING, ChangeOutcome
from .salesforce_org_fake import INSTANCE_URL, SalesforceOrgFake
from .watcher import run_record_change_watcher

SCOPE = Scope("salesforce-record-changes", "live")
CURSOR_KEY = PollCursorKey(
    "salesforce", CONNECTION_NAME, "recordModifiedPolled", RECORD_MODIFIED_TRIGGER_BINDING
)
# An inert fixture credential the fake org accepts; it is not a Salesforce session.
ACCESS_TOKEN = "inert-salesforce-" + "session"


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
            str(attributes["flow_id"])
            for _, message, attributes in self.records
            if message == "trigger event delivered"
        ]
        return list(dict.fromkeys(flow_ids))


@dataclass
class Environment:
    """One project over storage that survives a process restart, watching a run-unique object."""

    fake: SalesforceOrgFake
    s_object_type: str
    objects: InMemoryObjectStore = field(default_factory=InMemoryObjectStore)
    logger: RecordingLogger = field(default_factory=RecordingLogger)

    def project(self) -> LoadedProject:
        configuration = Configuration(
            SCOPE,
            1,
            (
                ConnectionConfiguration(
                    "salesforce",
                    CONNECTION_NAME,
                    "github.com/superdurable/dex-connectors-library/connectors/salesforce/go",
                    "salesforce",
                    {"pollInterval": 5_000_000_000, "pollPageSize": 2},
                    "salesforce-oauth",
                ),
            ),
            (
                {
                    "connectorId": "salesforce",
                    "connectionName": CONNECTION_NAME,
                    "triggerName": "recordModifiedPolled",
                    "bindingName": RECORD_MODIFIED_TRIGGER_BINDING,
                    "configuration": {"sObjectType": self.s_object_type, "fieldNames": ["Name", "StageName"]},
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
        """Save the authorized OAuth grant as Dex Web does, once for every later process."""
        material = CredentialMaterial(
            {
                "auth_method": "salesforce-oauth",
                "oauth_client_id": "inert-consumer-key",
                "oauth_client_secret": "inert-consumer-" + "secret",
                "access_token": ACCESS_TOKEN,
                "refresh_token": "inert-refresh-" + "grant",
                "instance_url": INSTANCE_URL,
            },
            None,
            "salesforce-oauth",
        )
        await ConnectionStore(self.objects, SCOPE).replace_credential(
            ConnectionKey("salesforce", CONNECTION_NAME), 0, material
        )

    async def is_seeded(self) -> bool:
        record = await self.project().poll_cursor_store(CURSOR_KEY).read_poll_cursor()
        return record.state is not None


@dataclass
class Process:
    """One run of the application; stopping it models a process exit."""

    client: dex.AsyncClient
    runner: ProjectRecordTriggerRunner
    cancellation: TriggerCancellation
    task: "asyncio.Task[None]"

    async def stop(self) -> None:
        self.cancellation.cancel()
        await self.task


@pytest.fixture
async def http_clients() -> AsyncIterator[list[httpx.AsyncClient]]:
    clients: list[httpx.AsyncClient] = []
    yield clients
    for client in clients:
        await client.aclose()


async def new_environment() -> Environment:
    # A custom object unique to the run keeps Trigger event IDs, and so Flow IDs, unique on a shared Dex.
    environment = Environment(SalesforceOrgFake(ACCESS_TOKEN), f"Deal{time.time_ns()}__c")
    await environment.store_credential()
    return environment


async def start_process(environment: Environment, clients: list[httpx.AsyncClient]) -> Process:
    http_client = httpx.AsyncClient(transport=environment.fake.transport())
    clients.append(http_client)
    cancellation = TriggerCancellation()
    ready: asyncio.Future[tuple[dex.AsyncClient, ProjectRecordTriggerRunner]] = (
        asyncio.get_running_loop().create_future()
    )
    task = asyncio.create_task(
        run_record_change_watcher(
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
        outcomes[outcome.record_id] = outcome
    return outcomes


def field_value(outcome: ChangeOutcome, name: str) -> Any:
    assert outcome.current is not None and outcome.current.fields is not None
    return outcome.current.fields[name]


async def test_record_changes_start_one_flow_each_across_a_restart(
    http_clients: list[httpx.AsyncClient],
) -> None:
    """Existing records are a baseline, each change starts one Flow, and changes while stopped arrive.

    A bulk load sharing one SystemModstamp pages by Id, a REQUEST_LIMIT_EXCEEDED pause loses nothing,
    and a record deleted before the read-back completes as deleted.
    """
    environment = await new_environment()
    fake, s_object_type = environment.fake, environment.s_object_type
    renewal = fake.insert(s_object_type, {"Name": "Renewal", "StageName": "Prospecting"})
    doomed = fake.insert(s_object_type, {"Name": "Doomed", "StageName": "Prospecting"})
    first = await start_process(environment, http_clients)
    try:
        await eventually(environment.is_seeded)
        expansion = fake.insert(s_object_type, {"Name": "Expansion", "StageName": "Qualification"})
        fake.update(renewal, {"StageName": "Closed Won"})
        fake.update(doomed, {"StageName": "Closed Lost"})
        fake.delete_after_listing.add(doomed)
        first.runner.wake_poller(RECORD_MODIFIED_TRIGGER_BINDING)
        outcomes = await wait_for_outcomes(environment, first.client, 3)
        assert outcomes[expansion].change_kind == "created"
        assert field_value(outcomes[expansion], "Name") == "Expansion"
        assert outcomes[renewal].change_kind == "updated"
        assert field_value(outcomes[renewal], "StageName") == "Closed Won", "read back from Salesforce"
        assert outcomes[doomed].is_deleted and outcomes[doomed].current is None

        bulk = fake.insert_batch(s_object_type, 5)
        fake.rate_limit_next(1, "1")
        limited = fake.query_count
        first.runner.wake_poller(RECORD_MODIFIED_TRIGGER_BINDING)
        outcomes = await wait_for_outcomes(environment, first.client, 8)
        assert all(outcomes[record_id].change_kind == "created" for record_id in bulk)
        assert fake.query_count > limited + 1, "the poll after the pause queried again"
        assert any(
            message == "poll rate limited; pausing connection" for _, message, _ in environment.logger.records
        )
    finally:
        await first.stop()

    offline = fake.insert(s_object_type, {"Name": "Created while stopped"})
    fake.update(expansion, {"StageName": "Negotiation"})
    second = await start_process(environment, http_clients)
    try:
        second.runner.wake_poller(RECORD_MODIFIED_TRIGGER_BINDING)
        outcomes = await wait_for_outcomes(environment, second.client, 10)
        assert field_value(outcomes[offline], "Name") == "Created while stopped"
        assert field_value(outcomes[expansion], "StageName") == "Negotiation"
        queries = fake.query_count
        second.runner.wake_poller(RECORD_MODIFIED_TRIGGER_BINDING)
        await eventually(lambda: fake.query_count > queries)
        assert len(environment.logger.delivered_flow_ids()) == 10, "no change twice and never the baseline"
    finally:
        await second.stop()


async def test_changed_object_records_a_new_baseline(http_clients: list[httpx.AsyncClient]) -> None:
    """A cursor for another object is invalid, so the poll reseeds without delivering existing records."""
    environment = await new_environment()
    environment.fake.insert(environment.s_object_type, {"Name": "First object"})
    first = await start_process(environment, http_clients)
    try:
        await eventually(environment.is_seeded)
    finally:
        await first.stop()

    environment.s_object_type = f"Other{time.time_ns()}__c"
    existing = environment.fake.insert(environment.s_object_type, {"Name": "Already there"})
    second = await start_process(environment, http_clients)
    try:

        async def is_reseeded() -> bool:
            record = await environment.project().poll_cursor_store(CURSOR_KEY).read_poll_cursor()
            return record.state is not None and record.state.reset_count == 1

        await eventually(is_reseeded)
        after_reset = environment.fake.insert(environment.s_object_type, {"Name": "After the reset"})
        second.runner.wake_poller(RECORD_MODIFIED_TRIGGER_BINDING)
        outcomes = await wait_for_outcomes(environment, second.client, 1)
        assert field_value(outcomes[after_reset], "Name") == "After the reset"
        assert existing not in outcomes, "the new object's existing records are its baseline"
    finally:
        await second.stop()
