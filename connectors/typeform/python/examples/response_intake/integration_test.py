# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Typeform delivery, redelivery, filtering, and restart replay through a real Dex Worker."""

import asyncio
import base64
import hashlib
import hmac
import json
import os
import tempfile
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError, TriggerEvent
from dex_connectors.sdk.projectconfig import TriggerInboxKey, load_from_environment

from dex_connectors.typeform import (
    FormResponseEvent,
    ProjectResponseSubmittedTriggerRoute,
    new_project_response_submitted_endpoint_runner,
)

from .app import ResponseIntakeApplication, open_response_receiver
from .flow import BINDING_NAME, CONNECTION_NAME, ResponseIntakeFlow, Submission

SECRET = "integration-" + "signing-key"
INBOX_KEY = TriggerInboxKey("typeform", CONNECTION_NAME, "responseSubmitted", BINDING_NAME)


def write_project(directory: Path) -> Path:
    """Write a local project configuration in the same format Dex Web saves."""
    path = directory / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "typeform",
                        "connectionName": CONNECTION_NAME,
                        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/typeform/go",
                        "provider": "typeform",
                        "authMethodId": "personal-access-token",
                        "configuration": {},
                        "credentials": {"access_token": "inert-token", "webhook_secret": SECRET},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "typeform",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "responseSubmitted",
                        "bindingName": BINDING_NAME,
                        "configuration": {"formId": "formA"},
                    }
                ],
            }
        )
    )
    path.chmod(0o600)
    return path


def delivery(token: str, form_id: str = "formA", event_type: str = "form_response") -> bytes:
    """Encode one Typeform-shaped signed delivery."""
    return json.dumps(
        {
            "event_id": uuid4().hex,
            "event_type": event_type,
            "form_response": {
                "form_id": form_id,
                "token": token,
                "submitted_at": "2026-11-16T12:00:00Z",
                "landed_at": "2026-11-16T11:59:00Z",
                "answers": [
                    {"field": {"id": "q1", "type": "email"}, "type": "email", "email": "ada@example.com"}
                ],
                "definition": {
                    "id": form_id,
                    "title": "Intake",
                    "fields": [{"id": "q1", "ref": "contact", "title": "Email"}],
                },
            },
        },
        separators=(",", ":"),
    ).encode()


async def post(application: object, body: bytes, secret: str = SECRET) -> int:
    """Send one raw body with Typeform's signature format through the ASGI receiver."""
    digest = base64.b64encode(hmac.new(secret.encode(), body, hashlib.sha256).digest()).decode()
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=application),  # type: ignore[arg-type]
        base_url="http://intake",
    ) as client:
        response = await client.post(
            "/webhooks/typeform", content=body, headers={"Typeform-Signature": "sha256=" + digest}
        )
    return response.status_code


@asynccontextmanager
async def running(receiver: object) -> AsyncIterator[None]:
    """Run the binding until this scope ends."""
    context = TriggerContext()
    task = asyncio.create_task(receiver.run(context))  # type: ignore[attr-defined]
    try:
        yield
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


async def until_answered(application: object, body: bytes) -> int:
    """Retry 503 while the binding starts, like Typeform does."""
    for _ in range(500):
        status = await post(application, body)
        if status != 503:
            return status
        await asyncio.sleep(0.01)
    raise AssertionError("Typeform receiver did not start")


class HeldTarget:
    """Keep an inbox event pending while the first application process stops."""

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[FormResponseEvent]) -> None:
        """Wait for shutdown before any Flow start."""
        await context.cancellation.wait()
        context.cancellation.raise_if_cancelled()


async def wait_for_flow(client: dex.AsyncClient, flow_id: str) -> dex.FlowResult:
    """Poll for an asynchronously created Flow and wait for completion."""
    for _ in range(500):
        try:
            result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=20)), 30)
        except dex.FlowNotFoundError:
            await asyncio.sleep(0.02)
            continue
        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
        return result
    raise AssertionError(f"Flow {flow_id} did not start")


@pytest.mark.asyncio
async def test_verified_delivery_replays_after_restart_and_deduplicates(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Inbox 200 precedes Dex; restart delivers once and filters other forms."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    suffix = uuid4().hex[:12]
    replay_token, live_token = "R" + suffix, "L" + suffix
    with tempfile.TemporaryDirectory(prefix="typeform-python-response-intake-") as directory:
        monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(write_project(Path(directory))))
        first_project = await load_from_environment()
        held = await new_project_response_submitted_endpoint_runner(
            first_project, CONNECTION_NAME, [ProjectResponseSubmittedTriggerRoute(BINDING_NAME, HeldTarget())]
        )
        try:
            async with running(held):
                assert await until_answered(held, delivery(replay_token)) == 200
            assert len(await first_project.trigger_inbox(INBOX_KEY).pending()) == 1
        finally:
            await held.aclose()
            await first_project.close()

        project = await load_from_environment()
        flow = ResponseIntakeFlow()
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                receiver = await open_response_receiver(project, client, flow)
                application = ResponseIntakeApplication(receiver)
                try:
                    async with running(receiver):
                        replayed = await wait_for_flow(client, f"typeform-formA-{replay_token}")
                        assert replayed.single_output(Submission).response_token == replay_token
                        live = delivery(live_token)
                        assert await until_answered(application, live) == 200
                        started = await wait_for_flow(client, f"typeform-formA-{live_token}")
                        assert started.single_output(Submission).form_id == "formA"
                        run_id = (await client.describe_flow(f"typeform-formA-{live_token}")).run_id
                        assert await post(application, live) == 200
                        assert await post(application, delivery("O" + suffix, form_id="otherForm")) == 200
                        assert await post(application, live, "wrong-key") == 400
                        assert (await client.describe_flow(f"typeform-formA-{live_token}")).run_id == run_id
                        for _ in range(500):
                            if await project.trigger_inbox(INBOX_KEY).pending() == ():
                                break
                            await asyncio.sleep(0.01)
                        assert await project.trigger_inbox(INBOX_KEY).pending() == ()
                finally:
                    await receiver.aclose()
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await project.close()
