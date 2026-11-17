# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay an acknowledged signed Stripe event into a real Dex Flow after Trigger restart."""

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

from dex_connectors.stripe import (
    ProjectCheckoutSessionUpdatedTriggerRoute,
    new_project_checkout_session_webhook_runtime,
    new_project_connection,
)

from .flow import BINDING_NAME, CONNECTION_NAME, FLOW_ID_PREFIX, CheckoutEventFlow, CheckoutEventInput
from .receiver import READINESS_PATH, WEBHOOK_PATH, CheckoutWebhookApplication, new_checkout_event_runtime

SECRET = "whsec_inert_" + "signing_secret"
KEY = TriggerInboxKey("stripe", CONNECTION_NAME, "checkoutSessionUpdated", BINDING_NAME)


def write_connections(directory: Path) -> Path:
    """Write inert Stripe credentials and one Checkout event binding."""
    path = directory / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "stripe",
                        "connectionName": CONNECTION_NAME,
                        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/stripe",
                        "provider": "stripe",
                        "authMethodId": "api-key-with-webhook",
                        "configuration": {},
                        "credentials": {"secret_key": "sk_test_inert", "webhook_secret": SECRET},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "stripe",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "checkoutSessionUpdated",
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
    """Leave an acknowledged delivery pending until the next Trigger runtime starts."""

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[object]) -> None:
        """Simulate a temporarily unavailable application target."""
        raise RuntimeError("target unavailable")


async def wait_ready(webhook: httpx.AsyncClient, runtime_task: asyncio.Task[None]) -> None:
    """Poll until the Trigger accepts signed requests."""
    while (await webhook.get(READINESS_PATH)).status_code != 200:
        if runtime_task.done():
            await runtime_task
        await asyncio.sleep(0.05)


@pytest.mark.asyncio
async def test_acknowledged_event_replays_into_one_flow_after_restart() -> None:
    """A signed Stripe event survives target failure and redelivery uses its original Flow ID."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    with tempfile.TemporaryDirectory(prefix="stripe-checkout-event-") as directory_name:
        directory = Path(directory_name)
        project = await load_from_environment(
            {"DEX_CONNECTOR_CONFIG_FILE": str(write_connections(directory))}
        )
        connection = new_project_connection(project, CONNECTION_NAME)
        flow = CheckoutEventFlow(connection)
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
                        "id": event_id,
                        "object": "event",
                        "type": "checkout.session.completed",
                        "created": 1700000000,
                        "data": {
                            "object": {
                                "id": "cs_test_" + uuid4().hex,
                                "object": "checkout.session",
                                "payment_status": "paid",
                                "status": "complete",
                                "currency": "usd",
                                "amount_total": 7500,
                                "client_reference_id": "registration-123",
                                "customer_email": "sentinel@example.com",
                                "client_secret": "SENTINEL_SECRET",
                            }
                        },
                    }
                ).encode()
                timestamp = int(datetime.now(UTC).timestamp())
                signature = hmac.new(
                    SECRET.encode(), f"{timestamp}.".encode() + payload, hashlib.sha256
                ).hexdigest()
                headers = {"Stripe-Signature": f"t={timestamp},v1={signature}"}
                failing = new_project_checkout_session_webhook_runtime(
                    project,
                    connection,
                    [ProjectCheckoutSessionUpdatedTriggerRoute(BINDING_NAME, FailingTarget())],
                )
                first_cancel = TriggerCancellation()
                first_task = asyncio.create_task(failing.run(TriggerContext(first_cancel)))
                async with httpx.AsyncClient(
                    transport=httpx.ASGITransport(app=CheckoutWebhookApplication(failing)),
                    base_url="http://stripe.test",
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
                runtime = new_checkout_event_runtime(project, restarted_connection, client, flow)
                second_cancel = TriggerCancellation()
                second_task = asyncio.create_task(runtime.run(TriggerContext(second_cancel)))
                async with httpx.AsyncClient(
                    transport=httpx.ASGITransport(app=CheckoutWebhookApplication(runtime)),
                    base_url="http://stripe.test",
                ) as webhook:
                    await asyncio.wait_for(wait_ready(webhook, second_task), 15)
                    flow_id = FLOW_ID_PREFIX + event_id

                    async def completed() -> CheckoutEventInput:
                        while True:
                            try:
                                result = await client.wait_for_flow(flow_id, timedelta(seconds=20))
                                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                                return result.single_output(CheckoutEventInput)
                            except dex.FlowNotFoundError:
                                await asyncio.sleep(0.05)

                    recorded = await asyncio.wait_for(completed(), 30)
                    assert recorded.received.session.payment_status == "paid"
                    assert "SENTINEL_SECRET" not in repr(recorded)
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
