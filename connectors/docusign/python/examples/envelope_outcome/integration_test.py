# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver signed Connect envelope outcomes through the envelopeEventReceived Trigger into real Dex Flows."""

import asyncio
import base64
import hashlib
import hmac
import json
import os
import tempfile
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from dataclasses import dataclass
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import TriggerInboxKey, load_from_environment

from dex_connectors.docusign import new_project_connection

from .flow import (
    CONNECTION_NAME,
    ENVELOPE_OUTCOME_TRIGGER_BINDING,
    FLOW_ID_PREFIX,
    EnvelopeOutcome,
    EnvelopeOutcomeFlow,
)
from .receiver import (
    CONNECT_PATH,
    READINESS_PATH,
    ConnectWebhookApplication,
    new_envelope_outcome_endpoint_runner,
)

ACCOUNT_ID = "a4ec37d6-1111-2222-3333-143885c220e1"
MODULE_PATH = "github.com/superdurable/dex-connectors-library/connectors/docusign"
# Inert, split so secret scanners do not mistake the fixture for a credential.
HMAC_KEY = "inert-docusign-" + "connect-hmac-key"
INBOX = TriggerInboxKey(
    "docusign", CONNECTION_NAME, "envelopeEventReceived", ENVELOPE_OUTCOME_TRIGGER_BINDING
)


@dataclass
class RunningExample:
    """The example's Connect app, the Dex client, and the DocuSign stand-in's envelope reads."""

    connect: httpx.AsyncClient
    dex_client: dex.AsyncClient
    envelope_reads: list[str]

    async def deliver(self, body: str, key: str = HMAC_KEY) -> int:
        signature = base64.b64encode(hmac.new(key.encode(), body.encode(), hashlib.sha256).digest()).decode()
        response = await self.connect.post(
            CONNECT_PATH,
            content=body.encode(),
            headers={"Content-Type": "application/json", "X-DocuSign-Signature-1": signature},
        )
        return response.status_code


def connect_body(event: str, envelope_id: str, request_id: str | None) -> str:
    """Return a JSON SIM envelope event that includes envelope data and, optionally, the correlation field."""
    fields = (
        [] if request_id is None else [{"name": "dexSigningRequestId", "value": request_id, "show": "false"}]
    )
    summary = {"status": event.removeprefix("envelope-"), "customFields": {"textCustomFields": fields}}
    data = {"accountId": ACCOUNT_ID, "envelopeId": envelope_id.upper(), "envelopeSummary": summary}
    return json.dumps({"event": event, "generatedDateTime": "2026-10-04T13:00:00.1230000Z", "data": data})


def _docusign_stand_in(envelope_reads: list[str]) -> httpx.MockTransport:
    def respond(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/oauth/userinfo":
            account = {"account_id": ACCOUNT_ID, "is_default": True, "base_uri": "https://na3.docusign.net"}
            return httpx.Response(200, json={"accounts": [account]})
        envelope_id = request.url.path.rsplit("/", 1)[-1]
        envelope_reads.append(envelope_id)
        return httpx.Response(
            200,
            json={
                "envelopeId": envelope_id,
                "status": "completed",
                "completedDateTime": "2026-10-04T12:59:00Z",
            },
        )

    return httpx.MockTransport(respond)


def _write_connections(directory: Path) -> Path:
    path = directory / "connections.json"
    credentials = {
        "oauth_client_id": "inert-integration-key",
        "oauth_client_secret": "inert-client-secret",
        "access_token": "inert-access-token",
        "refresh_token": "inert-refresh-token",
        "connect_hmac_key": HMAC_KEY,
    }
    connection = {
        "connectorId": "docusign",
        "connectionName": CONNECTION_NAME,
        "modulePath": MODULE_PATH,
        "provider": "docusign",
        "authMethodId": "docusign-oauth",
        "configuration": {},
        "credentials": credentials,
        "credentialExpiresAt": "2099-01-01T00:00:00Z",
    }
    binding = {
        "connectorId": "docusign",
        "connectionName": CONNECTION_NAME,
        "triggerName": "envelopeEventReceived",
        "bindingName": ENVELOPE_OUTCOME_TRIGGER_BINDING,
        "configuration": {},
    }
    document = {
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [connection],
        "triggerBindings": [binding],
    }
    path.write_text(json.dumps(document))
    path.chmod(0o600)
    return path


@asynccontextmanager
async def running_example() -> AsyncIterator[RunningExample]:
    """Run the Worker, the Dex client, and the endpoint runner against DEX_FLOW_SERVICE_ADDRESS."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    envelope_reads: list[str] = []
    with tempfile.TemporaryDirectory(prefix="docusign-python-trigger-") as directory:
        connections_path = _write_connections(Path(directory))
        project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(connections_path)})
        provider = httpx.AsyncClient(transport=_docusign_stand_in(envelope_reads))
        connection = new_project_connection(project, CONNECTION_NAME, provider)
        flow = EnvelopeOutcomeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        cancellation = TriggerCancellation()
        try:
            await worker.wait_pull_registered(timeout=15)
            client_options = dex.ClientOptions(server_address=server_address)
            async with dex.AsyncClient(registry, cache, client_options) as client:
                runner = new_envelope_outcome_endpoint_runner(project, connection, client, flow)
                application = ConnectWebhookApplication(runner)
                running = asyncio.create_task(runner.run(TriggerContext(cancellation)))
                transport = httpx.ASGITransport(app=application)
                async with httpx.AsyncClient(transport=transport, base_url="http://connect.test") as connect:
                    while (await connect.get(READINESS_PATH)).status_code != 200:
                        await asyncio.sleep(0.05)
                    try:
                        yield RunningExample(connect, client, envelope_reads)
                    finally:
                        cancellation.cancel()
                        with pytest.raises(TriggerDeliveryAbortedError):
                            await running
                assert await project.trigger_inbox(INBOX).pending() == ()
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
            await project.close()


async def _completed_outcome(example: RunningExample, flow_id: str) -> EnvelopeOutcome:
    # A 200 means the event is in the inbox; the Trigger starts the Flow afterwards.
    async def started() -> None:
        while True:
            try:
                await example.dex_client.describe_flow(flow_id)
                return
            except dex.FlowNotFoundError:
                await asyncio.sleep(0.05)

    await asyncio.wait_for(started(), 30)
    result = await asyncio.wait_for(example.dex_client.wait_for_flow(flow_id, timedelta(seconds=30)), 40)
    assert result.status == dex.FlowStatus.COMPLETED, result.error_message
    return result.single_output(EnvelopeOutcome)


def _new_envelope_id() -> str:
    suffix = uuid4().hex
    return f"{suffix[:8]}-{suffix[8:12]}-{suffix[12:16]}-{suffix[16:20]}-{suffix[20:]}"


@pytest.mark.asyncio
async def test_signed_outcome_starts_one_flow_that_reads_the_envelope_back() -> None:
    """A signed completion starts one Flow; a Connect redelivery reuses it without a second read."""
    envelope_id = _new_envelope_id()
    flow_id = FLOW_ID_PREFIX + envelope_id + "-envelope-completed"
    async with running_example() as example:
        body = connect_body("envelope-completed", envelope_id, "opp-123")
        assert await example.deliver(body) == 200
        outcome = await _completed_outcome(example, flow_id)
        assert outcome.event == "envelope-completed"
        assert outcome.envelope_id == envelope_id
        assert outcome.signing_request_id == "opp-123"
        assert outcome.current.status == "completed"
        assert example.envelope_reads.count(envelope_id) == 1

        assert await example.deliver(body) == 200, "a Connect retry is acknowledged"
        assert await _completed_outcome(example, flow_id) == outcome
        assert example.envelope_reads.count(envelope_id) == 1


@pytest.mark.asyncio
async def test_forged_uncorrelated_and_other_events_start_nothing() -> None:
    """A bad signature answers 400; uncorrelated outcomes and other events are acknowledged unrecorded."""
    forged_id, uncorrelated_id, declined_id = _new_envelope_id(), _new_envelope_id(), _new_envelope_id()
    async with running_example() as example:
        forged = connect_body("envelope-completed", forged_id, "opp-forged")
        assert await example.deliver(forged, key="not-the-key") == 400
        assert await example.deliver(connect_body("envelope-voided", uncorrelated_id, None)) == 200
        assert await example.deliver(connect_body("recipient-completed", uncorrelated_id, "opp-1")) == 200
        assert await example.deliver(connect_body("envelope-declined", declined_id, "opp-456")) == 200
        declined = await _completed_outcome(example, FLOW_ID_PREFIX + declined_id + "-envelope-declined")
        assert declined.signing_request_id == "opp-456"
        for flow_id in (
            FLOW_ID_PREFIX + forged_id + "-envelope-completed",
            FLOW_ID_PREFIX + uncorrelated_id + "-envelope-voided",
        ):
            with pytest.raises(dex.FlowNotFoundError):
                await example.dex_client.describe_flow(flow_id)
        assert forged_id not in example.envelope_reads and uncorrelated_id not in example.envelope_reads
