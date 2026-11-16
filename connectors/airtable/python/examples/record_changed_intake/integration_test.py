# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver Airtable record changes through the recordChanged poll Trigger into real Dex Flows."""

import asyncio
import json
import os
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from pathlib import Path

import dex
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerTarget,
    new_dex_flow_trigger_target,
)
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    load_from_environment,
    new_durable_trigger_target,
)

from dex_connectors.airtable import (
    Client,
    Config,
    Credentials,
    RecordChangedEvent,
    RecordChangedTriggerConfiguration,
    RecordChangedTriggerRoute,
    RecordChangedTriggerRunnerConfig,
    decode_record_changed_event_payload,
    new_connection,
    new_project_connection,
    new_record_changed_trigger_runner,
)
from tests.airtable_table_fake import (
    ACCESS_TOKEN,
    BASE_ID,
    ENDPOINT,
    FIELD_ID,
    TABLE_ID,
    AirtableTableFake,
    ScriptedResponse,
    fake_record,
)

from .flow import (
    CONNECTION_NAME,
    RECORD_CHANGED_TRIGGER_BINDING,
    Intake,
    RecordChangedIntakeFlow,
    accept_changed_record,
    map_to_flow_input,
    resolve_flow_id,
)
from .main import new_record_changed_intake_runner

SCOPE = Scope("airtable-record-changed", "live")
INBOX_KEY = TriggerInboxKey("airtable", CONNECTION_NAME, "recordChanged", RECORD_CHANGED_TRIGGER_BINDING)
CONFIGURATION = RecordChangedTriggerConfiguration(
    base_id=BASE_ID, table_id=TABLE_ID, table_name="Leads", field_id=FIELD_ID, field_name="Last Modified"
)


async def eventually(condition_met: Callable[[], Awaitable[bool]], timeout: float = 30) -> None:
    """Poll a condition until it holds."""
    async with asyncio.timeout(timeout):
        while not await condition_met():
            await asyncio.sleep(0.05)


def unique_time() -> datetime:
    """A per-run base time, so Flow IDs from earlier runs against the same Dex Server never collide."""
    return datetime.fromtimestamp(time.time_ns() // 1_000_000 / 1000, UTC).replace(microsecond=0)


async def wait_for_started_flow(dex_client: dex.AsyncClient, flow_id: str) -> dex.FlowResult:
    """Wait until the Trigger starts flow_id, then for its result."""
    async with asyncio.timeout(30):
        while True:
            try:
                return await dex_client.wait_for_flow(flow_id, timedelta(seconds=30))
            except dex.FlowNotFoundError:
                await asyncio.sleep(0.1)


@dataclass
class DeliveryRecorder:
    """Starts each change's Flow with the example's target, then remembers the delivery."""

    start: TriggerTarget[RecordChangedEvent]
    delivered: dict[str, TriggerEvent[RecordChangedEvent]] = field(default_factory=dict)
    order: list[str] = field(default_factory=list)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[RecordChangedEvent]) -> None:
        await self.start.handle_trigger(context, event)
        if event.id not in self.delivered:
            self.order.append(event.id)
        self.delivered[event.id] = event

    async def wait_for_outcomes(self, dex_client: dex.AsyncClient, count: int) -> list[Intake]:
        """Wait for count deliveries, then return each completed Flow's output in delivery order."""

        async def has_deliveries() -> bool:
            return len(self.delivered) >= count

        await eventually(has_deliveries)
        outcomes = []
        for event_id in self.order:
            event = self.delivered[event_id]
            result = await dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcomes.append(result.single_output(Intake))
        return outcomes


@asynccontextmanager
async def dex_environment(flow: RecordChangedIntakeFlow) -> AsyncIterator[dex.AsyncClient]:
    """Run a real Worker and Client against DEX_FLOW_SERVICE_ADDRESS."""
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    registry = dex.Registry([flow], allow_async_handlers=True)
    directory = Path(os.environ.get("TMPDIR", "/tmp")) / f"airtable-record-changed-{time.time_ns()}"
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory), 1 << 26))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            yield client
    finally:
        await worker.stop()
        await worker_task
        cache.close()


@asynccontextmanager
async def trigger_process(runner: PollRunner) -> AsyncIterator[PollRunner]:
    """Run one Trigger process until the block exits, as a restartable replica."""
    cancellation = TriggerCancellation()
    running = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        yield runner
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running


@pytest.mark.asyncio
async def test_changes_start_one_flow_each_across_a_renamed_field_and_a_restart() -> None:
    """Existing records start no Flow, each create and edit starts one, a renamed field keeps working, and
    a change made while the process is stopped arrives after a restart whose first request answers 503."""
    base_time = unique_time()
    table = AirtableTableFake(fake_record("recExisting000001", base_time))
    reference = ConnectionRef("airtable", CONNECTION_NAME)
    credentials = StaticCredentialProvider({reference: Credentials(SecretString(ACCESS_TOKEN))})
    connection = new_connection(
        Client(Config(endpoint=ENDPOINT, poll_interval_seconds=1), credentials, table.http_client()),
        reference,
    )
    flow = RecordChangedIntakeFlow(connection)
    objects = InMemoryObjectStore()
    cursor_store = PollCursorStore(objects, SCOPE, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))
    try:
        async with dex_environment(flow) as dex_client:
            recorder = DeliveryRecorder(
                new_dex_flow_trigger_target(
                    dex_client,
                    flow,
                    accepts_event=accept_changed_record,
                    resolve_flow_id=resolve_flow_id,
                    map_to_flow_input=map_to_flow_input,
                )
            )

            def new_runner() -> PollRunner:
                # The project runner's wiring: the inbox records each change before the cursor commits.
                target = new_durable_trigger_target(
                    TriggerInbox(objects, SCOPE, INBOX_KEY),
                    recorder,
                    decode_payload=decode_record_changed_event_payload,
                )
                route = RecordChangedTriggerRoute(
                    RECORD_CHANGED_TRIGGER_BINDING, CONFIGURATION, cursor_store, target
                )
                return new_record_changed_trigger_runner(
                    RecordChangedTriggerRunnerConfig(connection, [route])
                )

            async def is_seeded() -> bool:
                return (await cursor_store.read_poll_cursor()).state is not None

            async with trigger_process(new_runner()) as first:
                await eventually(is_seeded)
                edited = fake_record("recExisting000001", base_time + timedelta(seconds=2))
                table.put(fake_record("recNewLead0000001", base_time + timedelta(seconds=1)), edited)
                first.wake_poller(RECORD_CHANGED_TRIGGER_BINDING)
                outcomes = await recorder.wait_for_outcomes(dex_client, 2)
                assert [outcome.change.record_id for outcome in outcomes] == [
                    "recNewLead0000001",
                    "recExisting000001",
                ]
                assert (
                    outcomes[0].change.is_created_record
                    and outcomes[0].change.fields["Email"] == "recNewLead0000001@example.test"
                )
                assert outcomes[1].change.last_modified_time == base_time + timedelta(seconds=2)
                # Each Flow read its record back through the connection's getRecord Step.
                assert all(outcome.is_completed and not outcome.is_removed for outcome in outcomes)
                assert outcomes[0].current_fields["Email"] == "recNewLead0000001@example.test"

                table.rename_field("Updated At")
                table.put(fake_record("recRenamed0000001", base_time + timedelta(seconds=3)))
                first.wake_poller(RECORD_CHANGED_TRIGGER_BINDING)
                outcomes = await recorder.wait_for_outcomes(dex_client, 3)
                assert outcomes[2].change.record_id == "recRenamed0000001"
                assert outcomes[2].change.fields["Updated At"] and outcomes[2].current_fields["Updated At"]

            table.put(fake_record("recWhileStopped01", base_time + timedelta(seconds=4)))
            table.script(ScriptedResponse(503, error_type="SERVICE_UNAVAILABLE"))
            async with trigger_process(new_runner()) as second:
                second.wake_poller(RECORD_CHANGED_TRIGGER_BINDING)
                outcomes = await recorder.wait_for_outcomes(dex_client, 4)
                assert outcomes[3].change.record_id == "recWhileStopped01"
            assert len(recorder.delivered) == 4, "no change is delivered twice and existing records never"
            assert 503 in table.statuses, "the restarted poller recovered from a failed request"
            assert await TriggerInbox(objects, SCOPE, INBOX_KEY).pending() == ()
    finally:
        await connection.aclose()


@pytest.mark.asyncio
async def test_the_example_runner_polls_the_binding_saved_in_the_project(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """The Worker entrypoint's runner loads the saved binding and starts one Flow per new record."""
    base_time = unique_time()
    table = AirtableTableFake(fake_record("recExisting000001", base_time))
    connections_path = tmp_path / "connections.json"
    binding_configuration = {
        "baseId": BASE_ID,
        "baseName": "Intake",
        "tableId": TABLE_ID,
        "tableName": "Leads",
        "fieldId": FIELD_ID,
        "fieldName": "Last Modified",
    }
    connections_path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "airtable",
                        "connectionName": CONNECTION_NAME,
                        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/airtable",
                        "provider": "airtable",
                        "authMethodId": "api-key-only",
                        "configuration": {"endpoint": ENDPOINT, "pollInterval": 1_000_000_000},
                        "credentials": {"personal_access_token": ACCESS_TOKEN},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "airtable",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "recordChanged",
                        "bindingName": RECORD_CHANGED_TRIGGER_BINDING,
                        "configuration": binding_configuration,
                    }
                ],
            }
        )
    )
    connections_path.chmod(0o600)
    monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(connections_path))
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME, table.http_client())
    flow = RecordChangedIntakeFlow(connection)
    try:
        async with dex_environment(flow) as dex_client:
            runner = new_record_changed_intake_runner(project, connection, dex_client, flow)
            async with trigger_process(runner):

                async def has_seeded() -> bool:
                    return len(table.list_requests) >= 1

                await eventually(has_seeded)
                table.put(fake_record("recNewLead0000001", base_time + timedelta(seconds=1)))
                event_id = "recNewLead0000001-" + (base_time + timedelta(seconds=1)).strftime("%Y%m%dT%H%M%S")
                flow_id = "airtable-record-change-" + event_id + "000000000Z"
                result = await wait_for_started_flow(dex_client, flow_id)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                intake = result.single_output(Intake)
                assert intake.change.record_id == "recNewLead0000001" and intake.is_completed
                assert intake.current_fields["Email"] == "recNewLead0000001@example.test"
    finally:
        await connection.aclose()
        await project.close()
