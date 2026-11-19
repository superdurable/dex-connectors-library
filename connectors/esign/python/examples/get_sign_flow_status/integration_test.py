# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify signed eSign status across a real Dex Worker and Client retry."""

import asyncio
import os
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.esign import (
    Client,
    Config,
    Credentials,
    GetSignFlowStatusInput,
    GetSignFlowStatusOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, GetSignFlowStatusFlow


@pytest.mark.asyncio
async def test_sign_flow_status_retries_503(tmp_path: Path) -> None:
    """Run signed status lookup through real Dex retry and persisted completion."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url == httpx.URL("https://openapi.esign.cn/v3/sign-flow/flow123/detail")
        assert request.headers["X-Tsign-Open-App-Id"] == "app-123"
        assert request.headers["X-Tsign-Open-Auth-Mode"] == "Signature"
        assert request.headers["X-Tsign-Open-Ca-Signature"] == "C9P4BkL3d1BJ0EE390AVvnpUMYhDTix72iBm4yWPiZs="
        timestamp = datetime.fromtimestamp(int(request.headers["X-Tsign-Open-Ca-Timestamp"]) / 1000, UTC)
        assert abs((datetime.now(UTC) - timestamp).total_seconds()) < 60
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "code": 0,
                "data": {
                    "signFlowStatus": 1,
                    "signers": [{"accountMobile": "private-phone"}],
                    "docs": [{"fileName": "private-contract"}],
                },
            },
        )

    reference = ConnectionRef("esign", CONNECTION_NAME)
    credentials = Credentials("app-123", SecretString("test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = GetSignFlowStatusFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"esign-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetSignFlowStatusInput("flow123"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetSignFlowStatusOutput)
                assert output == GetSignFlowStatusOutput("flow123", 1)
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
