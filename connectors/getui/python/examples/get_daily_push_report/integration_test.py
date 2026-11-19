# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Getui Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.getui import (
    Client,
    Config,
    Credentials,
    GetDailyPushReportInput,
    GetDailyPushReportOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, DailyPushReportFlow


@pytest.mark.asyncio
async def test_get_daily_push_report_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        if len(requests) == 2:
            return httpx.Response(
                200, json={"code": 0, "data": {"token": "synthetic-token", "expire_time": "4102444800000"}}
            )
        return httpx.Response(
            200,
            json={"code": 0, "data": {"2021-01-01": {"total": {
                "request_num": 10, "msg_num": 4, "target_num": 4,
                "receive_num": 4, "display_num": 2, "click_num": 2,
            }}}},
        )

    reference = ConnectionRef("getui", CONNECTION_NAME)
    credentials = Credentials(
        "synthetic-app-id", SecretString("synthetic-app-key"), SecretString("synthetic-master-secret")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = DailyPushReportFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"getui-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetDailyPushReportInput(date="2021-01-01"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetDailyPushReportOutput)
                assert output.request_count == 10
                assert output.clicked_count == 2
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
