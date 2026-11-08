# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Greenhouse Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.greenhouse import (
    CandidateProfile,
    Client,
    Config,
    Credentials,
    GetCandidateInput,
    new_connection,
)

from .flow import CONNECTION_NAME, CandidateInspectionFlow


@pytest.mark.asyncio
async def test_candidate_inspection_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/v3/candidates"
        assert dict(request.url.params) == {
            "fields": "id,first_name,last_name",
            "ids": "53883394",
            "per_page": "1",
        }
        assert request.headers["Authorization"] == "Bearer sample-access-token"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json=[{"id": 53883394, "first_name": "Sample", "last_name": "Candidate"}],
        )

    reference = ConnectionRef("greenhouse", CONNECTION_NAME)
    credentials = Credentials(
        "sample-client-id", SecretString("sample-client-secret"), SecretString("sample-access-token")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345/v3"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = CandidateInspectionFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"greenhouse-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetCandidateInput(candidate_id=53883394))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(CandidateProfile) == CandidateProfile(
                    53883394, "Sample", "Candidate"
                )
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
