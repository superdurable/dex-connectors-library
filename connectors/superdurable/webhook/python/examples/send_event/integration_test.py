# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise signed delivery, retry, and result routing in real Dex."""

import asyncio
import base64
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
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.superdurable_webhook import SendEventOutput, new_project_connection

from .flow import CONNECTION_NAME, SendEventFlow
from .start import start_send_event


@pytest.mark.asyncio
async def test_signed_event_retries_with_one_id_through_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """Verify a real Worker retries 503 and completes on receiver acknowledgment."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    secret = "inert-webhook-signing-key"
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "POST"
        assert str(request.url) == "https://receiver.example.test/hooks?tenant=forms"
        assert request.headers["Content-Type"] == "application/json"
        assert json.loads(request.content) == {
            "type": "form.submitted",
            "data": {"submissionId": "evt_123"},
        }
        signed = (
            request.headers["webhook-id"].encode()
            + b"."
            + request.headers["webhook-timestamp"].encode()
            + b"."
            + request.content
        )
        expected_signature = (
            "v1," + base64.b64encode(hmac.new(secret.encode(), signed, hashlib.sha256).digest()).decode()
        )
        assert request.headers["webhook-signature"] == expected_signature
        return httpx.Response(503 if len(requests) == 1 else 202)

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="webhook-python-dex-") as cache_directory:
            connections_path = Path(cache_directory) / "connections.json"
            connections_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "webhook",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
                                ),
                                "provider": "webhook",
                                "authMethodId": "api-key-only",
                                "configuration": {
                                    "deliveryUrl": "https://receiver.example.test/hooks?tenant=forms"
                                },
                                "credentials": {"signing_secret": secret},
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            connections_path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(connections_path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(connections_path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_client)
            registry = dex.Registry([SendEventFlow(connection)], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        flow_id = f"webhook-python-send-{uuid4().hex}"
                        assert await start_send_event(flow_id, Path(__file__).with_name("sample-input.json"))
                        result = await asyncio.wait_for(
                            dex_client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        output = result.single_output(SendEventOutput)
                        assert output.status_code == 202
                        assert output.webhook_id.startswith("msg_")
                        assert len(requests) == 2
                        assert requests[0].headers["webhook-id"] == requests[1].headers["webhook-id"]
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
