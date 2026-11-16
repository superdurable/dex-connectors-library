# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver signed Calendly webhooks through the example receiver to a real Dex Worker and Flow."""

import asyncio
import hashlib
import hmac
import json
import os
import tempfile
import time
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError, TriggerEvent
from dex_connectors.sdk.projectconfig import LoadedProject, TriggerInboxKey, load_from_environment

from dex_connectors.calendly import (
    InviteeEvent,
    ProjectInviteeEventReceivedTriggerRoute,
    new_project_connection,
    new_project_invitee_event_received_endpoint_runner,
)

from .app import InviteeRecorderApplication, open_invitee_receiver
from .flow import BINDING_NAME, CONNECTION_NAME, InviteeRecorderFlow, RecordedScheduledEvent

# Split so that secret scanners do not mistake the inert test values for credentials.
SIGNING_KEY = "integration-" + "signing-key"
ACCESS_TOKEN = "inert-" + "access-token"
API = "https://api.calendly.com"
TYPE_URI = f"{API}/event_types/TYPE0001"
INBOX_KEY = TriggerInboxKey("calendly", CONNECTION_NAME, "inviteeEventReceived", BINDING_NAME)


def write_project(directory: Path) -> Path:
    """Write the owner-only connections.json that Dex Web saves for the connection and binding."""
    path = directory / "connections.json"
    connection = {
        "connectorId": "calendly",
        "connectionName": CONNECTION_NAME,
        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/calendly/go",
        "provider": "calendly",
        "authMethodId": "personal-access-token",
        "configuration": {},
        "credentials": {"access_token": ACCESS_TOKEN, "webhook_signing_key": SIGNING_KEY},
    }
    binding = {
        "connectorId": "calendly",
        "connectionName": CONNECTION_NAME,
        "triggerName": "inviteeEventReceived",
        "bindingName": BINDING_NAME,
        "configuration": {"eventTypeUri": TYPE_URI},
    }
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [connection],
                "triggerBindings": [binding],
            }
        )
    )
    path.chmod(0o600)
    return path


def scheduled_event(event_id: str, type_uri: str = TYPE_URI) -> dict[str, object]:
    """A Calendly scheduled event resource."""
    return {
        "uri": f"{API}/scheduled_events/{event_id}",
        "name": "Discovery Call",
        "status": "active",
        "start_time": "2026-11-02T17:00:00.000000Z",
        "end_time": "2026-11-02T17:30:00.000000Z",
        "event_type": type_uri,
        "invitees_counter": {"total": 1, "active": 1, "limit": 1},
    }


def invitee_webhook(event: str, event_id: str, invitee_id: str, type_uri: str = TYPE_URI) -> bytes:
    """Encode an invitee webhook as Calendly sends it."""
    payload: dict[str, object] = {
        "uri": f"{API}/scheduled_events/{event_id}/invitees/{invitee_id}",
        "event": f"{API}/scheduled_events/{event_id}",
        "email": "ada@example.com",
        "name": "Ada Lovelace",
        "status": "active" if event == "invitee.created" else "canceled",
        "scheduled_event": scheduled_event(event_id, type_uri),
    }
    envelope = {"event": event, "created_at": "2026-11-01T10:00:00.000000Z", "payload": payload}
    return json.dumps(envelope).encode()


class CalendlyStandIn:
    """Answers GET /scheduled_events/<id> and counts the reads."""

    def __init__(self) -> None:
        self.reads: list[str] = []

    def handle(self, request: httpx.Request) -> httpx.Response:
        assert request.headers["authorization"] == "Bearer " + ACCESS_TOKEN
        event_id = request.url.path.rsplit("/", 1)[-1]
        self.reads.append(event_id)
        return httpx.Response(200, json={"resource": scheduled_event(event_id)})


async def post_signed(application: object, body: bytes, key: str = SIGNING_KEY) -> int:
    """Post one webhook through the ASGI application, signed as Calendly signs it."""
    timestamp = str(int(time.time()))
    digest = hmac.new(key.encode(), timestamp.encode() + b"." + body, hashlib.sha256).hexdigest()
    transport = httpx.ASGITransport(app=application)  # type: ignore[arg-type]
    async with httpx.AsyncClient(transport=transport, base_url="http://recorder") as http_client:
        response = await http_client.post(
            "/webhooks/calendly",
            content=body,
            headers={"Calendly-Webhook-Signature": f"t={timestamp},v1={digest}"},
        )
    return response.status_code


async def until_answered(application: object, body: bytes) -> int:
    """Retry like Calendly while the binding is still starting (503)."""
    for _ in range(1000):
        status = await post_signed(application, body)
        if status != 503:
            return status
        await asyncio.sleep(0.01)
    raise AssertionError("receiver did not start")


@asynccontextmanager
async def running(receiver: object) -> AsyncIterator[None]:
    """Run the receiver's binding until the block ends, as an ASGI lifespan does."""
    context = TriggerContext()
    task = asyncio.create_task(receiver.run(context))  # type: ignore[attr-defined]
    try:
        yield
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


async def wait_for_booking_flow(dex_client: dex.AsyncClient, flow_id: str) -> dex.FlowResult:
    """Wait for the Flow the receiver starts asynchronously to exist and complete."""
    for _ in range(1000):
        try:
            result = await asyncio.wait_for(dex_client.wait_for_flow(flow_id, timedelta(seconds=20)), 30)
        except dex.FlowNotFoundError:
            await asyncio.sleep(0.02)
            continue
        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
        return result
    raise AssertionError(f"Flow {flow_id} was not started")


class HeldTarget:
    """Never consumes, standing in for an application that stopped before its Flow started."""

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[InviteeEvent]) -> None:
        await context.cancellation.wait()
        context.cancellation.raise_if_cancelled()


async def record_without_consuming(body: bytes) -> None:
    """Act as a first process that records one booking and stops before its Flow starts."""
    project = await load_from_environment()
    route = ProjectInviteeEventReceivedTriggerRoute(BINDING_NAME, HeldTarget())
    held = await new_project_invitee_event_received_endpoint_runner(project, CONNECTION_NAME, [route])
    try:
        async with running(held):
            assert await until_answered(held, body) == 200
        assert len(await project.trigger_inbox(INBOX_KEY).pending()) == 1
    finally:
        await held.aclose()
        await project.close()


async def deliver_through_receiver(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: InviteeRecorderFlow,
    calendly: CalendlyStandIn,
    http_client: httpx.AsyncClient,
    suffix: str,
) -> None:
    """Replay the recorded booking, then deliver, deduplicate, refuse, and filter live webhooks."""
    receiver = await open_invitee_receiver(project, dex_client, flow, http_client)
    application = InviteeRecorderApplication(receiver)
    try:
        async with running(receiver):
            replayed = await wait_for_booking_flow(
                dex_client, f"calendly-invitee.created-R{suffix}-INVITEE01"
            )
            assert replayed.single_output(RecordedScheduledEvent).scheduled_event.uri.endswith(f"/R{suffix}")

            booked = invitee_webhook("invitee.created", f"B{suffix}", "INVITEE01")
            assert await until_answered(application, booked) == 200
            flow_id = f"calendly-invitee.created-B{suffix}-INVITEE01"
            recorded = (await wait_for_booking_flow(dex_client, flow_id)).single_output(
                RecordedScheduledEvent
            )
            assert recorded.branch == "found" and recorded.scheduled_event.name == "Discovery Call"
            started_run = (await dex_client.describe_flow(flow_id)).run_id

            # Calendly redelivers the same webhook, which is a duplicate start and no second read.
            assert await post_signed(application, booked) == 200
            tampered = booked.replace(b"Ada Lovelace", b"Eve")
            assert await post_signed(application, tampered, "another-key") == 400
            canceled = invitee_webhook("invitee.canceled", f"C{suffix}", "INVITEE01")
            assert await post_signed(application, canceled) == 200
            other_type = invitee_webhook(
                "invitee.created", f"T{suffix}", "INVITEE01", f"{API}/event_types/TYPE0002"
            )
            assert await post_signed(application, other_type) == 200
            routing = b'{"event":"routing_form_submission.created","payload":{}}'
            assert await post_signed(application, routing) == 200
            sentinel = invitee_webhook("invitee.created", f"S{suffix}", "INVITEE01")
            assert await post_signed(application, sentinel) == 200
            # One binding delivers in arrival order, so the sentinel completes last.
            await wait_for_booking_flow(dex_client, f"calendly-invitee.created-S{suffix}-INVITEE01")
            assert (await dex_client.describe_flow(flow_id)).run_id == started_run
            for skipped in (
                f"calendly-invitee.canceled-C{suffix}-INVITEE01",
                f"calendly-invitee.created-T{suffix}-INVITEE01",
            ):
                with pytest.raises(dex.FlowNotFoundError):
                    await dex_client.describe_flow(skipped)
            assert calendly.reads == [f"R{suffix}", f"B{suffix}", f"S{suffix}"]
            assert await project.trigger_inbox(INBOX_KEY).pending() == ()
    finally:
        await receiver.aclose()


@pytest.mark.asyncio
async def test_signed_bookings_start_one_flow_each_and_survive_a_restart(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A recorded booking replays after a restart; each new booking starts one Flow that reads its event."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    suffix = uuid4().hex[:16]
    calendly = CalendlyStandIn()
    with tempfile.TemporaryDirectory(prefix="calendly-python-invitee-recorder-") as directory:
        monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(write_project(Path(directory))))
        await record_without_consuming(invitee_webhook("invitee.created", f"R{suffix}", "INVITEE01"))
        project = await load_from_environment()
        async with httpx.AsyncClient(transport=httpx.MockTransport(calendly.handle)) as http_client:
            connection = new_project_connection(project, CONNECTION_NAME, http_client)
            flow = InviteeRecorderFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=15)
                client_options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, client_options) as dex_client:
                    await deliver_through_receiver(project, dex_client, flow, calendly, http_client, suffix)
            finally:
                await worker.stop()
                await worker_task
                cache.close()
                await project.close()
