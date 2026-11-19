# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Tuya Flow across a real Dex Worker and retry boundary."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.tuya import (
    Client,
    Config,
    Credentials,
    GetDeviceDetailsInput,
    GetDeviceDetailsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, DeviceDetailsFlow


@pytest.mark.asyncio
async def test_provider_query_retries_503(tmp_path: Path) -> None:
    """Persist safe metadata after one device-service availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    paths: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        paths.append(request.url.path)
        assert request.headers["sign_method"] == "HMAC-SHA256"
        assert len(request.headers["sign"]) == 64
        if request.url.path == "/v1.0/token":
            return httpx.Response(
                200,
                json={
                    "success": True,
                    "result": {
                        "access_token": "sample-token",
                        "expire_time": 7200,
                    },
                },
            )
        assert request.url.path == "/v1.0/devices/device123"
        assert request.headers["access_token"] == "sample-token"
        if paths.count(request.url.path) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "success": True,
                "result": {
                    "id": "device123",
                    "name": "Body fat scale",
                    "online": True,
                    "category": "qt",
                    "product_id": "product42",
                    "create_time": 1560827137,
                    "update_time": 1589764585,
                    "local_key": "discard-me",
                    "status": [{"code": "sensitive"}],
                },
            },
        )

    reference = ConnectionRef("tuya", CONNECTION_NAME)
    credentials = Credentials(SecretString("sample-client-id"), SecretString("sample-client-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = DeviceDetailsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"tuya-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetDeviceDetailsInput("device123"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetDeviceDetailsOutput)
                assert output.device_id == "device123"
                assert output.online is True
                assert "discard-me" not in repr(output)
                assert "sensitive" not in repr(output)
                assert len(paths) == 4
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
