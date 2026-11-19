# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Aqara retry and persistence across a real Dex Worker and Client."""

import asyncio
import json
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.aqara import (
    Client,
    Config,
    Credentials,
    DevicePage,
    ListDevicesInput,
    new_connection,
)

from .flow import CONNECTION_NAME, DeviceListFlow


@pytest.mark.asyncio
async def test_device_list_retries_503(tmp_path: Path) -> None:
    """Persist one list after the provider's first attempt returns 503."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "POST"
        assert request.url.path == "/v3.0/open/api"
        assert request.headers["Accesstoken"] == "access-token"
        assert request.headers["Appid"] == "app-id"
        assert request.headers["Keyid"] == "key-id"
        assert request.headers["Sign"]
        assert json.loads(request.content) == {
            "intent": "query.device.info",
            "data": {"pageNum": 2, "pageSize": 25},
        }
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "code": 0,
                "result": {
                    "data": [
                        {
                            "did": "lumi.1",
                            "model": "lumi.gateway.aqhm01",
                            "state": 1,
                            "deviceName": "Hub",
                            "modelType": 1,
                        }
                    ],
                    "totalCount": 30,
                },
            },
        )

    reference = ConnectionRef("aqara", CONNECTION_NAME)
    credentials = Credentials("app-id", "key-id", SecretString("app-key"), SecretString("access-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = DeviceListFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"aqara-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListDevicesInput(2, 25))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(DevicePage)
                assert len(output.devices) == 1
                assert output.devices[0].device_id == "lumi.1"
                assert output.total_count == 30
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
