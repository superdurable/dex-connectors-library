# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver BambooHR employee changes through the example's Flow target to a real Dex Worker and Flow."""

import asyncio
import json
import os
import tempfile
import time
from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any, cast

import dex
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
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import (
    Configuration,
    InMemoryObjectStore,
    LoadedProject,
    PollCursorKey,
    PollCursorState,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
)

from dex_connectors.bamboohr import (
    EMPLOYEE_CHANGE_SETTLE_WINDOW,
    Client,
    Config,
    Credentials,
    EmployeeChangedEvent,
    ProjectEmployeeChangedTriggerRoute,
    new_connection,
    new_project_employee_changed_trigger_runner,
)
from tests.bamboohr_fake import API_KEY, BambooHRFake

from .flow import BINDING_NAME, CONNECTION_NAME, EmployeeChangeFlow, EmployeeChangeOutcome, resolve_flow_id
from .worker import new_employee_change_flow_target

SCOPE = Scope("bamboohr-employee-changed", "live")
INBOX_KEY = TriggerInboxKey("bamboohr", CONNECTION_NAME, "employeeChanged", BINDING_NAME)


class DurableProject:
    """The project storage two Trigger processes share: hosted storage keeps inboxes and cursors."""

    def __init__(self, trigger_bindings: tuple[dict[str, Any], ...] = ()) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, (), trigger_bindings)

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)

    async def cursor_state(self) -> PollCursorState | None:
        return (
            await self.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(INBOX_KEY)).read_poll_cursor()
        ).state


@dataclass
class CountingTarget:
    """Wraps the example's Flow target and records each delivered event once, in order."""

    inner: Any
    delivered: dict[str, TriggerEvent[EmployeeChangedEvent]] = field(default_factory=dict)

    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[EmployeeChangedEvent]
    ) -> None:
        await self.inner.handle_trigger(context, event)
        self.delivered.setdefault(event.id, event)


async def eventually(condition: Callable[[], Any], timeout: float = 30) -> None:
    """Poll a sync or async condition until it holds."""
    deadline = time.monotonic() + timeout
    while True:
        result = condition()
        if asyncio.iscoroutine(result):
            result = await result
        if result:
            return
        assert time.monotonic() < deadline, "condition did not converge"
        await asyncio.sleep(0.05)


@asynccontextmanager
async def trigger_process(runner: PollRunner) -> AsyncIterator[PollRunner]:
    """Run one Trigger process until the block exits."""
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        yield runner
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


@dataclass
class Environment:
    provider: BambooHRFake
    project: DurableProject
    target: CountingTarget
    dex_client: dex.AsyncClient
    connection: Any
    first_employee_id: int

    def employee_id(self, offset: int) -> str:
        return str(self.first_employee_id + offset)

    def new_runner(self) -> PollRunner:
        routes = [ProjectEmployeeChangedTriggerRoute(BINDING_NAME, self.target)]
        return new_project_employee_changed_trigger_runner(
            cast(LoadedProject, self.project), self.connection, routes
        )

    async def wait_for_cursor(self) -> PollCursorState:
        await eventually(self.project.cursor_state)
        state = await self.project.cursor_state()
        assert state is not None
        return state

    async def wait_for_outcomes(self, count: int) -> dict[str, EmployeeChangeOutcome]:
        await eventually(lambda: len(self.target.delivered) >= count)
        outcomes: dict[str, EmployeeChangeOutcome] = {}
        for event in list(self.target.delivered.values()):
            result = await asyncio.wait_for(
                self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30)), 40
            )
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(EmployeeChangeOutcome)
            assert outcome.employee_id == event.payload.employee_id
            outcomes[outcome.employee_id] = outcome
        return outcomes


def field_of(outcome: EmployeeChangeOutcome, name: str) -> str:
    """One hand-off field of the employee the Flow read back."""
    assert outcome.current is not None and outcome.current.fields is not None
    return outcome.current.fields[name]


@asynccontextmanager
async def employee_changed_environment(
    trigger_bindings: tuple[dict[str, Any], ...] = (),
) -> AsyncIterator[Environment]:
    """Serve the example Flow on a real Dex Worker against a fake BambooHR company."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    # A unique clock and employee IDs keep Trigger event IDs, and so Flow IDs, unique on a shared server.
    provider = BambooHRFake(datetime.now(UTC) - timedelta(days=1))
    reference = ConnectionRef("bamboohr", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)) as http_client:
        client = Client(
            Config(company_domain="acme", poll_interval_seconds=10),
            StaticCredentialProvider({reference: Credentials(SecretString(API_KEY))}),
            http_client,
            "https://acme.example/api/v1",
        )
        connection = new_connection(client, reference)
        flow = EmployeeChangeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="bamboohr-employee-changed-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, options) as dex_client:
                    yield Environment(
                        provider,
                        DurableProject(trigger_bindings),
                        CountingTarget(new_employee_change_flow_target(dex_client, flow)),
                        dex_client,
                        connection,
                        1_000_000 + (time.time_ns() // 1000) % 1_000_000_000_000,
                    )
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_employee_changes_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """The seed starts nothing; each change starts one Flow; changes made while stopped arrive later."""
    async with employee_changed_environment() as environment:
        provider = environment.provider
        existing, hire, mover, leaver, offline = (environment.employee_id(offset) for offset in range(5))
        provider.insert(existing, {"firstName": "Ava"})
        provider.insert(mover, {"firstName": "Sam", "department": "Sales"})
        provider.insert(leaver, {"firstName": "Lee"})
        provider.advance(timedelta(seconds=1))
        async with trigger_process(environment.new_runner()) as first:
            await environment.wait_for_cursor()
            provider.insert(hire, {"firstName": "Noa", "lastName": "Kim", "workEmail": "noa@example.com"})
            provider.update(mover, {"department": "Engineering"})
            provider.delete(leaver)
            provider.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
            first.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(3)
        assert outcomes[hire].action == "Inserted"
        assert outcomes[hire].current is not None
        assert field_of(outcomes[hire], "workEmail") == "noa@example.com", "the Flow reads the employee back"
        assert outcomes[mover].action == "Updated"
        assert outcomes[mover].current is not None
        assert field_of(outcomes[mover], "department") == "Engineering"
        assert outcomes[leaver].action == "Deleted" and outcomes[leaver].is_gone
        assert outcomes[leaver].current is None

        provider.insert(offline, {"firstName": "Planned while stopped"})
        provider.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
        async with trigger_process(environment.new_runner()) as second:
            second.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(4)
        assert outcomes[offline].current is not None
        assert field_of(outcomes[offline], "firstName") == "Planned while stopped"
        assert existing not in outcomes, "the change before the seed never starts a Flow"
        assert len(environment.target.delivered) == 4, "no change twice"
        state = await environment.project.cursor_state()
        assert state is not None
        assert json.loads(state.token)["afterEmployeeId"] == offline


@pytest.mark.asyncio
async def test_saved_change_type_and_a_rejected_cursor_reseed_with_real_dex() -> None:
    """The saved type filters BambooHR's list; a 400 starts again at the current time, skipping the gap."""
    saved = {
        "connectorId": "bamboohr",
        "connectionName": CONNECTION_NAME,
        "triggerName": "employeeChanged",
        "bindingName": BINDING_NAME,
        "configuration": {"changeType": "inserted"},
    }
    async with employee_changed_environment((saved,)) as environment:
        provider = environment.provider
        hire, edited, lost, later = (environment.employee_id(offset) for offset in range(4))
        provider.insert(edited, {"firstName": "Ava"})
        async with trigger_process(environment.new_runner()) as runner:
            await environment.wait_for_cursor()
            provider.insert(hire, {"firstName": "Noa"})
            provider.update(edited, {"department": "Sales"})
            provider.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
            runner.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(1)
            assert list(outcomes) == [hire], "the saved inserted type skips the update"

            provider.insert(lost, {"firstName": "Lost in the gap"})
            provider.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
            provider.reject_change_lists(1)
            runner.wake_poller(BINDING_NAME)

            async def is_reset() -> bool:
                state = await environment.project.cursor_state()
                return state is not None and state.reset_count == 1

            await eventually(is_reset)
            provider.insert(later, {"firstName": "After the reset"})
            provider.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
            runner.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(2)
        assert sorted(outcomes) == sorted([hire, later]), "the change in the gap is not delivered"
        assert all(query.get("type") == "inserted" for query in provider.change_list_queries)
