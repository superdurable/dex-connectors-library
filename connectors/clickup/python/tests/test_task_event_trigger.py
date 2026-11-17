# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify ClickUp signature, event decoding, filtering, and durable delivery."""

import asyncio
import hashlib
import hmac
import json
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from pathlib import Path

import pytest
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError, TriggerEvent
from dex_connectors.sdk.projectconfig import TriggerInboxKey, load_from_environment

from dex_connectors.clickup import (
    ProjectTaskEventTriggerRoute,
    new_project_connection,
    new_project_task_event_endpoint_runner,
)

CONNECTION_NAME = "clickup-workspace"
BINDING_NAME = "task-events"
KEY = TriggerInboxKey("clickup", CONNECTION_NAME, "taskEvent", BINDING_NAME)
SECRET = "inert-clickup-" + "webhook-secret"


class CollectingTarget:
    """Collect delivered events after the project's durable inbox."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[object]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[object]) -> None:
        """Record one delivered event."""
        await self.events.put(event)


async def load_project(directory: Path, events: list[str] | None = None, secret: str = SECRET):
    """Load a local Dex Web connection with a configured taskEvent binding."""
    path = directory / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "clickup",
                        "connectionName": CONNECTION_NAME,
                        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/clickup",
                        "provider": "clickup",
                        "configuration": {"webhookMaxBodyBytes": 4096},
                        "credentials": {"api_token": "pk_inert_test_token", "webhook_secret": secret},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "clickup",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "taskEvent",
                        "bindingName": BINDING_NAME,
                        "configuration": {"events": events or []},
                    }
                ],
            }
        )
    )
    path.chmod(0o600)
    return await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})


@asynccontextmanager
async def running(
    directory: Path, target: CollectingTarget, events: list[str] | None = None, secret: str = SECRET
) -> AsyncIterator:
    project = await load_project(directory, events, secret)
    connection = new_project_connection(project, CONNECTION_NAME)
    runner = new_project_task_event_endpoint_runner(
        project, connection, [ProjectTaskEventTriggerRoute(BINDING_NAME, target)]
    )
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:

        async def ready() -> None:
            while runner.running_source_count() != 1:
                if task.done():
                    await task
                await asyncio.sleep(0.01)

        await asyncio.wait_for(ready(), 5)
        yield runner, project
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await connection.aclose()
        await project.close()


def body(event: str = "taskStatusUpdated", item_id: str = "history-1") -> bytes:
    """Encode one ClickUp task event with status history."""
    return json.dumps(
        {
            "event": event,
            "task_id": "task_123",
            "webhook_id": "webhook-123",
            "history_items": [
                {
                    "id": item_id,
                    "field": "status",
                    "date": "1642734631523",
                    "user": {"id": 183},
                    "parent_id": "list-1",
                    "before": {"status": "in progress"},
                    "after": {"status": "blocked"},
                }
            ],
        }
    ).encode()


def signed(payload: bytes, key: str = SECRET) -> list[tuple[str, str]]:
    """Return the ClickUp X-Signature for the exact request bytes."""
    return [("X-Signature", hmac.new(key.encode(), payload, hashlib.sha256).hexdigest())]


@pytest.mark.asyncio
async def test_signed_delivery_and_rejection(tmp_path: Path) -> None:
    target = CollectingTarget()
    async with running(tmp_path, target, ["taskStatusUpdated"]) as (runner, project):
        payload = body()
        assert (await runner.handle_request("POST", "/clickup/task-events", [], payload)).status == 400
        assert (
            await runner.handle_request("POST", "/clickup/task-events", signed(payload, "wrong"), payload)
        ).status == 400
        assert (
            await runner.handle_request("POST", "/clickup/task-events", signed(payload), payload + b" ")
        ).status == 400
        assert (await runner.handle_request("POST", "/clickup/task-events", [], b" " * 5000)).status == 413
        other = body("listCreated")
        assert (
            await runner.handle_request("POST", "/clickup/task-events", signed(other), other)
        ).status == 200
        filtered = body("taskCreated")
        assert (
            await runner.handle_request("POST", "/clickup/task-events", signed(filtered), filtered)
        ).status == 200
        assert (
            await runner.handle_request("POST", "/clickup/task-events", signed(payload), payload)
        ).status == 200
        event = await asyncio.wait_for(target.events.get(), 5)
        assert event.id == "webhook-123:taskStatusUpdated:history-1"
        assert event.payload.history_items[0].status_before == "in progress"
        assert event.payload.history_items[0].status_after == "blocked"
        assert event.payload.history_items[0].user_id == 183
        assert (
            await runner.handle_request("POST", "/clickup/task-events", signed(payload), payload)
        ).status == 200
        assert (await asyncio.wait_for(target.events.get(), 5)).id == event.id
        assert await project.trigger_inbox(KEY).pending() == ()


@pytest.mark.asyncio
async def test_missing_secret_requests_retry(tmp_path: Path) -> None:
    async with running(tmp_path, CollectingTarget(), secret="") as (runner, _):
        payload = body()
        assert (
            await runner.handle_request("POST", "/clickup/task-events", signed(payload), payload)
        ).status == 503


@pytest.mark.asyncio
async def test_replays_acknowledged_event_after_restart(tmp_path: Path) -> None:
    """An event acknowledged before a failed target is replayed on the next runner."""

    class FailingTarget:
        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[object]) -> None:
            raise RuntimeError("target unavailable")

    payload = body(item_id="history-restart")
    async with running(tmp_path, FailingTarget()) as (runner, project):
        assert (
            await runner.handle_request("POST", "/clickup/task-events", signed(payload), payload)
        ).status == 200

        async def persisted() -> None:
            while not await project.trigger_inbox(KEY).pending():
                await asyncio.sleep(0.01)

        await asyncio.wait_for(persisted(), 5)
    replayed = CollectingTarget()
    async with running(tmp_path, replayed):
        event = await asyncio.wait_for(replayed.events.get(), 5)
    assert event.id == "webhook-123:taskStatusUpdated:history-restart"


@pytest.mark.asyncio
async def test_deleted_event_and_malformed_payloads(tmp_path: Path) -> None:
    """A deleted task needs no history; malformed signed task payloads are refused."""
    target = CollectingTarget()
    async with running(tmp_path, target) as (runner, _):
        deleted = json.dumps(
            {"event": "taskDeleted", "task_id": "task_123", "webhook_id": "webhook-123"}
        ).encode()
        assert (
            await runner.handle_request("POST", "/clickup/task-events", signed(deleted), deleted)
        ).status == 200
        event = await asyncio.wait_for(target.events.get(), 5)
        assert event.id == "webhook-123:taskDeleted:task_123"
        assert event.payload.history_items == ()
        for payload in (
            b'{"event":',
            b'{"event":"taskCreated","task_id":"task_123"}',
            b'{"event":"taskCreated","webhook_id":"webhook-123"}',
            b'{"event":"taskCreated","task_id":"task_123","webhook_id":"webhook-123","history_items":[{"id":"bad:id"}]}',
        ):
            assert (
                await runner.handle_request("POST", "/clickup/task-events", signed(payload), payload)
            ).status == 400
