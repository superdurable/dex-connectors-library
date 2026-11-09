# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Azure DevOps Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.microsoft_azure_devops import (
    Client,
    Config,
    Credentials,
    ListProjectsInput,
    Projects,
    new_connection,
)

from .flow import CONNECTION_NAME, ProjectDiscoveryFlow


@pytest.mark.asyncio
async def test_project_discovery_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Basic OnN5bnRoZXRpYy1hcGkta2V5"
        assert request.url.path == "/example/_apis/projects"
        assert request.url.query == b"%24top=20&%24skip=0&api-version=7.1"
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={"count": 1, "value": [{"id": "eb6e4656-77fc-42a1-9181-4c6d8e9da5d1",
                                   "name": "Example", "state": "wellFormed"}]},
        )

    reference = ConnectionRef("azure-devops", CONNECTION_NAME)
    credentials = Credentials(SecretString("synthetic-api-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345/example"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = ProjectDiscoveryFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"azure-devops-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListProjectsInput(top=20, skip=0))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(Projects).projects[0].name == "Example"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
