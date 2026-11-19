# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify EMQX deployment listing across a real Dex Worker and Client retry."""

import asyncio
import base64
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.emq import (
    Client,
    Config,
    Credentials,
    ListDeploymentsInput,
    ListDeploymentsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, DeploymentListFlow


@pytest.mark.asyncio
async def test_deployment_list_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow through real Dex retry and persisted completion."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.path == "/public_api/v1/deployments"
        assert not request.url.query and not request.content
        encoded = base64.b64encode(b"test-platform-key:test-platform-secret").decode("ascii")
        assert request.headers["Authorization"] == "Basic " + encoded
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json=[
                {
                    "connections": 1000,
                    "tps": 1000,
                    "createAt": "2024-11-21 02:51",
                    "deploymentID": "mdfba81f",
                    "deploymentName": "deployment-mdfba81f",
                    "deploymentType": "serverless",
                    "platform": "AWS",
                    "projectName": "default",
                    "region": "N. Virginia (us-east-1)",
                    "status": "stopped",
                    "version": "v5",
                    "private": "private-data",
                }
            ],
        )

    reference = ConnectionRef("emq", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-platform-key"), SecretString("test-platform-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(platform_endpoint="http://127.0.0.1:12345/public_api/v1"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = DeploymentListFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"emq-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListDeploymentsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListDeploymentsOutput)
                assert len(output.deployments) == 1
                assert output.deployments[0].deployment_id == "mdfba81f"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
