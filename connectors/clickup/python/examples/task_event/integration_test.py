# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver signed ClickUp webhooks into a real Dex Worker and restart the durable Trigger."""

import asyncio
import hashlib
import hmac
import json
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import TriggerInboxKey, load_from_environment

from dex_connectors.clickup import new_project_connection

from .flow import BINDING_NAME, CONNECTION_NAME, FLOW_ID_PREFIX, TaskEventFlow, TaskEventInput
from .receiver import (
    READINESS_PATH,
    WEBHOOK_PATH,
    TaskEventWebhookApplication,
    new_task_event_endpoint_runner,
)

SECRET = "inert-clickup-" + "webhook-secret"
KEY = TriggerInboxKey("clickup", CONNECTION_NAME, "taskEvent", BINDING_NAME)


def write_connections(directory: Path) -> Path:
    """Write inert local ClickUp connection and Trigger binding settings."""
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
                        "configuration": {},
                        "credentials": {"api_token": "pk_inert_test_token", "webhook_secret": SECRET},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "clickup",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "taskEvent",
                        "bindingName": BINDING_NAME,
                        "configuration": {"events": ["taskStatusUpdated"]},
                    }
                ],
            }
        )
    )
    path.chmod(0o600)
    return path


@pytest.mark.asyncio
async def test_signed_event_starts_one_flow_and_redelivery_reuses_it() -> None:
    """A signed delivery starts a Flow; duplicate ClickUp delivery finds its stable ID."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    with tempfile.TemporaryDirectory(prefix="clickup-task-event-") as directory_name:
        directory = Path(directory_name)
        project = await load_from_environment(
            {"DEX_CONNECTOR_CONFIG_FILE": str(write_connections(directory))}
        )
        connection = new_project_connection(project, CONNECTION_NAME)
        flow = TaskEventFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        cancellation = TriggerCancellation()
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                runner = new_task_event_endpoint_runner(project, connection, client, flow)
                running = asyncio.create_task(runner.run(TriggerContext(cancellation)))
                application = TaskEventWebhookApplication(runner)
                async with httpx.AsyncClient(
                    transport=httpx.ASGITransport(app=application), base_url="http://clickup.test"
                ) as webhook:

                    async def ready() -> None:
                        while (await webhook.get(READINESS_PATH)).status_code != 200:
                            if running.done():
                                await running
                            await asyncio.sleep(0.05)

                    await asyncio.wait_for(ready(), 15)
                    item_id = uuid4().hex
                    payload = json.dumps(
                        {
                            "event": "taskStatusUpdated",
                            "task_id": "task_123",
                            "webhook_id": "webhook-123",
                            "history_items": [
                                {
                                    "id": item_id,
                                    "field": "status",
                                    "date": "1642734631523",
                                    "before": {"status": "open"},
                                    "after": {"status": "blocked"},
                                }
                            ],
                        }
                    ).encode()
                    signature = hmac.new(SECRET.encode(), payload, hashlib.sha256).hexdigest()
                    headers = {"X-Signature": signature}
                    assert (
                        await webhook.post(WEBHOOK_PATH, content=payload, headers=headers)
                    ).status_code == 200
                    flow_id = FLOW_ID_PREFIX + "webhook-123-taskStatusUpdated-" + item_id

                    async def completed() -> TaskEventInput:
                        while True:
                            try:
                                result = await client.wait_for_flow(flow_id, timedelta(seconds=20))
                                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                                return result.single_output(TaskEventInput)
                            except dex.FlowNotFoundError:
                                await asyncio.sleep(0.05)

                    recorded = await asyncio.wait_for(completed(), 30)
                    assert recorded.received.history_items[0].status_after == "blocked"
                    assert (
                        await webhook.post(WEBHOOK_PATH, content=payload, headers=headers)
                    ).status_code == 200
                    assert await asyncio.wait_for(completed(), 30) == recorded
                cancellation.cancel()
                with pytest.raises(TriggerDeliveryAbortedError):
                    await running
                assert await project.trigger_inbox(KEY).pending() == ()
        finally:
            cancellation.cancel()
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
            await project.close()
