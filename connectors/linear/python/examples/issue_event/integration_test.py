# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay an acknowledged Linear webhook after Trigger restart into a real Dex Flow."""

import asyncio
import hashlib
import hmac
import json
import os
import tempfile
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError, TriggerEvent
from dex_connectors.sdk.projectconfig import TriggerInboxKey, load_from_environment

from dex_connectors.linear import (
    ProjectIssueEventReceivedTriggerRoute,
    new_project_connection,
    new_project_issue_event_received_endpoint_runner,
)

from .flow import BINDING_NAME, CONNECTION_NAME, FLOW_ID_PREFIX, IssueEventFlow, IssueEventInput
from .receiver import (
    READINESS_PATH,
    WEBHOOK_PATH,
    IssueEventWebhookApplication,
    new_issue_event_endpoint_runner,
)

SECRET = "inert-linear-" + "webhook-signing-secret"
TEAM_ID = "7f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"
STATE_ID = "8f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"
KEY = TriggerInboxKey("linear", CONNECTION_NAME, "issueEventReceived", BINDING_NAME)


def write_connections(directory: Path) -> Path:
    """Write an inert Linear key and signed webhook secret for the example."""
    path = directory / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "linear",
                        "connectionName": CONNECTION_NAME,
                        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/linear",
                        "provider": "linear",
                        "authMethodId": "personal-api-key",
                        "configuration": {},
                        "credentials": {"api_key": "lin_api_inert_key", "webhook_signing_secret": SECRET},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "linear",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "issueEventReceived",
                        "bindingName": BINDING_NAME,
                        "configuration": {},
                    }
                ],
            }
        )
    )
    path.chmod(0o600)
    return path


class FailingTarget:
    """Keep an acknowledged delivery pending so a restarted runner can replay it."""

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[object]) -> None:
        """Simulate an unavailable application target."""
        raise RuntimeError("target unavailable")


async def wait_ready(webhook: httpx.AsyncClient, runner_task: asyncio.Task[None]) -> None:
    """Poll until the Trigger source accepts deliveries."""
    while (await webhook.get(READINESS_PATH)).status_code != 200:
        if runner_task.done():
            await runner_task
        await asyncio.sleep(0.05)


@pytest.mark.asyncio
async def test_acknowledged_webhook_replays_after_restart_and_redelivery_reuses_flow() -> None:
    """A durable inbox outlives a failed target and the Worker runs the replayed event once."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    with tempfile.TemporaryDirectory(prefix="linear-issue-event-") as directory_name:
        directory = Path(directory_name)
        project = await load_from_environment(
            {"DEX_CONNECTOR_CONFIG_FILE": str(write_connections(directory))}
        )
        connection = new_project_connection(project, CONNECTION_NAME)
        flow = IssueEventFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                issue_id = str(uuid4())
                updated_at = datetime.now(UTC).replace(microsecond=123000)
                updated_text = updated_at.isoformat(timespec="milliseconds").replace("+00:00", "Z")
                payload = json.dumps(
                    {
                        "action": "update",
                        "type": "Issue",
                        "createdAt": updated_text,
                        "organizationId": "d4e5f6a7-b8c9-4d0e-9f1a-2b3c4d5e6f7a",
                        "webhookId": "e5f6a7b8-c9d0-4e1f-8a2b-3c4d5e6f7a8b",
                        "webhookTimestamp": int(datetime.now(UTC).timestamp() * 1000),
                        "data": {
                            "id": issue_id,
                            "identifier": "ENG-7",
                            "number": 7,
                            "title": "Fire panel wiring",
                            "url": "https://linear.app/acme/issue/ENG-7",
                            "teamId": TEAM_ID,
                            "stateId": STATE_ID,
                            "priority": 2,
                            "labelIds": [],
                            "createdAt": updated_text,
                            "updatedAt": updated_text,
                        },
                    }
                ).encode()
                signature = hmac.new(SECRET.encode(), payload, hashlib.sha256).hexdigest()
                headers = {"Linear-Signature": signature}
                failing = new_project_issue_event_received_endpoint_runner(
                    project,
                    connection,
                    [ProjectIssueEventReceivedTriggerRoute(BINDING_NAME, FailingTarget())],
                )
                first_cancel = TriggerCancellation()
                first_task = asyncio.create_task(failing.run(TriggerContext(first_cancel)))
                async with httpx.AsyncClient(
                    transport=httpx.ASGITransport(app=IssueEventWebhookApplication(failing)),
                    base_url="http://linear.test",
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
                runner = new_issue_event_endpoint_runner(project, restarted_connection, client, flow)
                second_cancel = TriggerCancellation()
                second_task = asyncio.create_task(runner.run(TriggerContext(second_cancel)))
                async with httpx.AsyncClient(
                    transport=httpx.ASGITransport(app=IssueEventWebhookApplication(runner)),
                    base_url="http://linear.test",
                ) as webhook:
                    await asyncio.wait_for(wait_ready(webhook, second_task), 15)
                    event_id = f"update:{issue_id}:{int(updated_at.timestamp() * 1000)}"
                    flow_id = FLOW_ID_PREFIX + event_id.replace(":", "-")

                    async def completed() -> IssueEventInput:
                        while True:
                            try:
                                result = await client.wait_for_flow(flow_id, timedelta(seconds=20))
                                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                                return result.single_output(IssueEventInput)
                            except dex.FlowNotFoundError:
                                await asyncio.sleep(0.05)

                    recorded = await asyncio.wait_for(completed(), 30)
                    assert recorded.received.issue.id == issue_id
                    assert recorded.received.issue.title == "Fire panel wiring"
                    assert (
                        await webhook.post(WEBHOOK_PATH, content=payload, headers=headers)
                    ).status_code == 200
                    assert await asyncio.wait_for(completed(), 30) == recorded
                second_cancel.cancel()
                with pytest.raises(TriggerDeliveryAbortedError):
                    await second_task
                assert await project.trigger_inbox(KEY).pending() == ()
                await restarted_connection.aclose()
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
            await project.close()
