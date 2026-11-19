# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Alibaba Cloud Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.alibaba import (
    Client,
    Config,
    Credentials,
    DescribeRegionsInput,
    DescribeRegionsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, RegionsFlow


@pytest.mark.asyncio
async def test_regions_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.headers["x-acs-action"] == "DescribeRegions"
        assert request.headers["Authorization"].startswith("ACS3-HMAC-SHA256 Credential=test-access-key-id,")
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "Regions": {
                    "Region": [
                        {
                            "RegionId": "cn-hangzhou",
                            "LocalName": "华东1（杭州）",
                            "RegionEndpoint": "ecs.cn-hangzhou.aliyuncs.com",
                            "Status": "available",
                        }
                    ]
                }
            },
        )

    reference = ConnectionRef("alibaba", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-access-key-id"), SecretString("test-access-key-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = RegionsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"alibaba-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, DescribeRegionsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(DescribeRegionsOutput).regions[0].region_id == "cn-hangzhou"
                assert len(requests) == 2
                assert (
                    requests[0].headers["x-acs-signature-nonce"]
                    != requests[1].headers["x-acs-signature-nonce"]
                )
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
