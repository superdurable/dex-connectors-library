# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Creatio Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.creatio import (
    Client,
    Config,
    ContactSummary,
    Credentials,
    GetContactInput,
    new_connection,
)

from .flow import CONNECTION_NAME, ContactInspectionFlow


@pytest.mark.asyncio
async def test_contact_inspection_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    contact_id = "90a68db5-cb3a-4f64-bb60-c0a934ee7600"
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/0/odata/Contact"
        assert request.url.params["$filter"] == "Id eq " + contact_id
        assert request.url.params["$select"] == "Id,Name,Email"
        assert request.url.params["$top"] == "1"
        assert request.headers["Authorization"] == "Bearer sample-access-token"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={"value": [{"Id": contact_id, "Name": "Ada Lovelace", "Email": "ada@example.com"}]},
        )

    reference = ConnectionRef("creatio", CONNECTION_NAME)
    credentials = Credentials(
        "sample-client-id", SecretString("sample-client-secret"), SecretString("sample-access-token")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345", token_endpoint="http://127.0.0.1:12345/connect/token"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = ContactInspectionFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"creatio-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetContactInput(contact_id=contact_id))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(ContactSummary) == ContactSummary(
                    contact_id, "Ada Lovelace", "ada@example.com"
                )
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
