# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Microsoft Dynamics 365 Sales Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.microsoft_dynamics_365_sales import (
    Client,
    Config,
    Contact,
    Credentials,
    FindContactInput,
    new_connection,
)

from .flow import CONNECTION_NAME, ContactFlow


@pytest.mark.asyncio
async def test_contact_lookup_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    contact_attempts = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal contact_attempts
        requests.append(request)
        if request.url.path == "/token":
            assert request.method == "POST"
            return httpx.Response(200, json={"access_token": "test-access-token", "token_type": "Bearer"})
        assert request.method == "GET"
        assert request.url.path == "/api/data/v9.2/contacts"
        assert request.headers["Authorization"] == "Bearer test-access-token"
        contact_attempts += 1
        if contact_attempts == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "value": [
                    {
                        "contactid": "33333333-3333-4333-8333-333333333333",
                        "fullname": "Ada Example",
                        "emailaddress1": "ada@example.com",
                    }
                ]
            },
        )

    reference = ConnectionRef("microsoft-dynamics-365-sales", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-client-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(
                environment_url="http://127.0.0.1:12345",
                tenant_id="11111111-1111-4111-8111-111111111111",
                client_id="22222222-2222-4222-8222-222222222222",
            ),
            StaticCredentialProvider({reference: credentials}),
            http_client,
            token_endpoint="http://127.0.0.1:12345/token",
        )
        connection = new_connection(provider, reference)
        flow = ContactFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"dynamics365sales-python-{uuid4().hex}"
                await client.start_flow(
                    flow,
                    flow_id,
                    FindContactInput(email_address="ada@example.com"),
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(Contact).contact_id == "33333333-3333-4333-8333-333333333333"
                assert contact_attempts == 2
                assert len(requests) == 4
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
