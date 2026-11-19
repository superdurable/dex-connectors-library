# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Sensors Data Flow across a real Dex Worker and retry boundary."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.sensorsdata import (
    Client,
    Config,
    Credentials,
    ListCampaignsInput,
    ListCampaignsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, CampaignCatalogFlow


@pytest.mark.asyncio
async def test_provider_query_retries_503(tmp_path: Path) -> None:
    """Persist the provider result after one availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/api/v3/analytics/v1/channel/campaigns/list"
        assert request.headers["api-key"] == "k" * 35
        assert request.headers["sensorsdata-project"] == "production"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "code": "SUCCESS",
                "data": {
                    "campaign_details": [
                        {
                            "campaign_name": "测试活动",
                            "branch_num": 1,
                            "latest_use_time": "2025-01-01 12:00:00",
                        }
                    ],
                    "total_rows": "6",
                    "total_page": 1,
                    "page_num": 1,
                    "page_size": 20,
                },
            },
        )

    reference = ConnectionRef("sensorsdata", CONNECTION_NAME)
    credentials = Credentials(SecretString("k" * 35))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="https://family.demo.sensorsdata.cn", project_name="production"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = CampaignCatalogFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"sensorsdata-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListCampaignsInput(1, 20))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListCampaignsOutput)
                assert output.total_rows == 6
                assert output.campaigns[0].name == "测试活动"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
