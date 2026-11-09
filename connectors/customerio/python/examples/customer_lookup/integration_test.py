# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Customerio Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.customerio import (
    Client,
    Config,
    Credentials,
    CustomerLookupPage,
    GetCustomersByEmailInput,
    new_connection,
)

from .flow import CONNECTION_NAME, CustomerLookupFlow


@pytest.mark.asyncio
async def test_customer_lookup_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/v1/customers"
        assert request.headers["Authorization"] == "Bearer sample-customerio-private-key"
        assert request.url.params["email"] == "person@example.com"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "results": [{"cio_id": "a3000001", "email": "person@example.com", "id": 2}],
            },
        )

    reference = ConnectionRef("customerio", CONNECTION_NAME)
    credentials = Credentials(SecretString("sample-customerio-private-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = CustomerLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"customerio-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetCustomersByEmailInput(email="person@example.com"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(CustomerLookupPage).email == "person@example.com"
                assert result.single_output(CustomerLookupPage).customer_ids == ("a3000001",)
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
