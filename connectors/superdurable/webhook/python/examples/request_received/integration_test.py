# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay one acknowledged webhook into a real Dex Flow after Trigger restart."""

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
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError, TriggerEvent
from dex_connectors.sdk.projectconfig import TriggerInboxKey, load_from_environment

from dex_connectors.superdurable_webhook import (
    ProjectRequestReceivedTriggerRoute,
    new_project_connection,
    new_project_request_received_webhook_runtime,
)

from .flow import BINDING_NAME, CONNECTION_NAME, FLOW_ID_PREFIX, SubmissionFlow, SubmissionInput
from .receiver import (
    READINESS_PATH,
    WEBHOOK_PATH,
    SubmissionWebhookApplication,
    new_submission_runtime,
)

SECRET = "inert-form-signing-secret"
KEY = TriggerInboxKey("webhook", CONNECTION_NAME, "requestReceived", BINDING_NAME)


def write_connections(directory: Path) -> Path:
    """Write inert local credentials and one static binding."""
    path = directory / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "webhook",
                        "connectionName": CONNECTION_NAME,
                        "modulePath": (
                            "github.com/superdurable/dex-connectors-library/"
                            "connectors/superdurable/webhook"
                        ),
                        "provider": "webhook",
                        "authMethodId": "webhook-signing-secret",
                        "configuration": {"eventIdPointer": "/event_id", "forwardedHeaders": ["X-Form-Type"]},
                        "credentials": {"signing_secret": SECRET},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "webhook",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "requestReceived",
                        "bindingName": BINDING_NAME,
                        "configuration": {"matchPointer": "/type", "matchValues": ["form.submitted"]},
                    }
                ],
            }
        )
    )
    path.chmod(0o600)
    return path


class FailingTarget:
    """Leave an acknowledged event pending until the receiver restarts."""

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[object]) -> None:
        """Simulate an unavailable application target."""
        raise RuntimeError("target unavailable")


async def wait_ready(webhook: httpx.AsyncClient, task: asyncio.Task[None]) -> None:
    """Poll readiness until the binding has replayed its inbox."""
    while (await webhook.get(READINESS_PATH)).status_code != 200:
        if task.done():
            await task
        await asyncio.sleep(0.05)


@pytest.mark.asyncio
async def test_acknowledged_request_replays_into_one_flow_after_restart() -> None:
    """A verified request survives target failure; redelivery keeps one Flow ID."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    with tempfile.TemporaryDirectory(prefix="webhook-form-submission-") as directory_name:
        directory = Path(directory_name)
        project = await load_from_environment(
            {"DEX_CONNECTOR_CONFIG_FILE": str(write_connections(directory))}
        )
        connection = new_project_connection(project, CONNECTION_NAME)
        flow = SubmissionFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        restarted_connection = None
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                event_id = "evt_" + uuid4().hex
                payload = json.dumps(
                    {
                        "event_id": event_id,
                        "type": "form.submitted",
                        "data": {"name": "Ada Lovelace"},
                    }
                ).encode()
                signature = hmac.new(SECRET.encode(), payload, hashlib.sha256).hexdigest()
                headers = {
                    "Content-Type": "application/json",
                    "X-Signature-256": signature,
                    "X-Form-Type": "registration",
                    "Authorization": "Bearer SENTINEL_TOKEN",
                }
                failing = new_project_request_received_webhook_runtime(
                    project, connection, [ProjectRequestReceivedTriggerRoute(BINDING_NAME, FailingTarget())]
                )
                first_cancel = TriggerCancellation()
                first_task = asyncio.create_task(failing.run(TriggerContext(first_cancel)))
                async with httpx.AsyncClient(
                    transport=httpx.ASGITransport(app=SubmissionWebhookApplication(failing)),
                    base_url="http://webhook.test",
                ) as webhook:
                    await asyncio.wait_for(wait_ready(webhook, first_task), 15)
                    assert (
                        await webhook.post(WEBHOOK_PATH, content=payload, headers=headers)
                    ).status_code == 200
                    assert len(await project.trigger_inbox(KEY).pending()) == 1
                first_cancel.cancel()
                with pytest.raises(TriggerDeliveryAbortedError):
                    await first_task

                restarted_connection = new_project_connection(project, CONNECTION_NAME)
                runtime = new_submission_runtime(project, restarted_connection, client, flow)
                second_cancel = TriggerCancellation()
                second_task = asyncio.create_task(runtime.run(TriggerContext(second_cancel)))
                async with httpx.AsyncClient(
                    transport=httpx.ASGITransport(app=SubmissionWebhookApplication(runtime)),
                    base_url="http://webhook.test",
                ) as webhook:
                    await asyncio.wait_for(wait_ready(webhook, second_task), 15)
                    flow_id = FLOW_ID_PREFIX + event_id

                    async def completed() -> SubmissionInput:
                        while True:
                            try:
                                result = await client.wait_for_flow(flow_id, timedelta(seconds=20))
                                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                                return result.single_output(SubmissionInput)
                            except dex.FlowNotFoundError:
                                await asyncio.sleep(0.05)

                    recorded = await asyncio.wait_for(completed(), 30)
                    assert recorded.received.json_body["data"]["name"] == "Ada Lovelace"
                    assert recorded.received.headers == {"X-Form-Type": "registration"}
                    assert "SENTINEL_TOKEN" not in repr(recorded)
                    assert (
                        await webhook.post(WEBHOOK_PATH, content=payload, headers=headers)
                    ).status_code == 200
                    assert await asyncio.wait_for(completed(), 30) == recorded
                second_cancel.cancel()
                with pytest.raises(TriggerDeliveryAbortedError):
                    await second_task
                assert await project.trigger_inbox(KEY).pending() == ()
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            if restarted_connection is not None:
                await restarted_connection.aclose()
            await connection.aclose()
            await project.close()
