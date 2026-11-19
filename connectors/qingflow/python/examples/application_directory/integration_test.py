# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise Qingflow retry and persistence across a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.qingflow import (
    Client,
    Config,
    Credentials,
    ListApplicationsInput,
    ListApplicationsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ApplicationDirectoryFlow


@pytest.mark.asyncio
async def test_application_directory_retries_503(tmp_path: Path) -> None:
    """Complete and persist the application list after one retry."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/app"
        assert request.headers["accessToken"] == "private-test-key"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={"errCode": 0, "result": {"appList": [{"appKey": "app_example", "appName": "Operations"}]}},
        )

    reference = ConnectionRef("qingflow", CONNECTION_NAME)
    credentials = Credentials(SecretString("private-test-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = ApplicationDirectoryFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(
            registry, cache, dex.WorkerOptions(server_address=os.environ["DEX_FLOW_SERVICE_ADDRESS"])
        )
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=os.environ["DEX_FLOW_SERVICE_ADDRESS"])
            ) as client:
                flow_id = f"qingflow-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListApplicationsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListApplicationsOutput)
                assert len(output.applications) == 1
                assert output.applications[0].app_key == "app_example"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
