# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Marketo Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.adobe_marketo import (
    Client,
    Config,
    Credentials,
    GetLeadInput,
    LeadProfile,
    new_connection,
)
from examples.lead_inspection.flow import CONNECTION_NAME, LeadInspectionFlow


@pytest.mark.asyncio
async def test_lead_inspection_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/rest/v1/lead/318581.json"
        assert dict(request.url.params) == {
            "fields": "id,firstName,lastName",
        }
        assert request.headers["Authorization"] == "Bearer sample-access-token"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={"success": True, "result": [{"id": 318581, "firstName": "Sample", "lastName": "Lead"}]},
        )

    reference = ConnectionRef("adobe", CONNECTION_NAME)
    credentials = Credentials(
        "sample-client-id", SecretString("sample-client-secret"), SecretString("sample-access-token")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(
                rest_endpoint="https://123-ABC-456.mktorest.com/rest",
                token_endpoint="https://123-ABC-456.mktorest.com/identity/oauth/token",
            ),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = LeadInspectionFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"adobe-marketo-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetLeadInput(lead_id=318581))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(LeadProfile) == LeadProfile(318581, "Sample", "Lead")
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
