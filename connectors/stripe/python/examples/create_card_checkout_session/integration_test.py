# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the card Checkout example through a real Dex Worker and Client."""

import asyncio
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

from dex_connectors.stripe import CheckoutSession, new_project_connection

from .flow import CONNECTION_NAME, CardCheckoutFlow
from .start import start_card_checkout


@pytest.mark.asyncio
async def test_card_checkout_retries_and_fails_unwired_rejection_in_real_dex(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Cover durable Step routing, stable keyed retry, and optional branch failure."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        form = dict(httpx.QueryParams(request.content))
        if form["client_reference_id"] == "rejected":
            return httpx.Response(400, json={"error": {"type": "invalid_request_error"}})
        if len(requests) == 1:
            return httpx.Response(429, headers={"Retry-After": "1"})
        return httpx.Response(
            200,
            json={
                "id": "cs_test_123",
                "object": "checkout.session",
                "status": "open",
                "currency": "usd",
                "url": "https://checkout.stripe.com/test",
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        with tempfile.TemporaryDirectory(prefix="stripe-python-dex-blobs-") as cache_directory:
            connections_path = Path(cache_directory) / "connections.json"
            connections_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "stripe",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/stripe"
                                ),
                                "provider": "stripe",
                                "authMethodId": "api-key-only",
                                "configuration": {},
                                "credentials": {"secret_key": "sk_test_inert"},
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            connections_path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(connections_path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(connections_path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_http_client)
            flow = CardCheckoutFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(
                dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24)
            )
            try:
                worker = dex.AsyncWorker(
                    registry, cache, dex.WorkerOptions(server_address=server_address)
                )
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        successful_flow_id = f"stripe-python-card-{uuid4().hex}"
                        sample_input = Path(__file__).with_name("sample-input.json")
                        assert await start_card_checkout(successful_flow_id, sample_input)
                        success = await asyncio.wait_for(
                            dex_client.wait_for_flow(successful_flow_id, timedelta(seconds=20)), 30
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        session = success.single_output(CheckoutSession)
                        assert session.id == "cs_test_123"
                        assert session.url == "https://checkout.stripe.com/test"
                        assert len(requests) == 2
                        first_key = requests[0].headers["Idempotency-Key"]
                        assert first_key == requests[1].headers["Idempotency-Key"]
                        assert first_key

                        rejected_flow_id = f"stripe-python-card-{uuid4().hex}"
                        rejected_input = json.loads(sample_input.read_text())
                        rejected_input["clientReferenceId"] = "rejected"
                        rejected_input_path = Path(cache_directory) / "rejected-input.json"
                        rejected_input_path.write_text(json.dumps(rejected_input))
                        assert await start_card_checkout(rejected_flow_id, rejected_input_path)
                        rejected = await asyncio.wait_for(
                            dex_client.wait_for_flow(rejected_flow_id, timedelta(seconds=20)), 30
                        )
                        assert rejected.status == dex.FlowStatus.FAILED
                        assert rejected.error_message is not None
                        assert "providerRejected" in rejected.error_message
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
