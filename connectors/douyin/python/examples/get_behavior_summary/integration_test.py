# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify token reuse and retry across a real Dex Worker and Client boundary."""

import asyncio
import json
import os
from datetime import UTC, datetime, timedelta, timezone
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.douyin import (
    Client,
    Config,
    Credentials,
    GetBehaviorSummaryInput,
    GetBehaviorSummaryOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, GetBehaviorSummaryFlow


@pytest.mark.asyncio
async def test_behavior_summary_retries_503_with_cached_token(tmp_path: Path) -> None:
    """Use one token across two real Dex Execute attempts and persist only counts."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    report_day = datetime.now(UTC).astimezone(timezone(timedelta(hours=8))).date() - timedelta(days=2)
    token_requests: list[httpx.Request] = []
    report_requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/oauth/client_token/":
            token_requests.append(request)
            assert request.method == "POST"
            assert json.loads(request.content) == {
                "grant_type": "client_credential",
                "client_key": "tt-test-app",
                "client_secret": "test-client-secret",
            }
            return httpx.Response(
                200, json={"data": {"access_token": "test-token", "error_code": 0, "expires_in": 7200}}
            )
        assert request.url.path == "/api/platform/v2/data_analysis/query_behavior_data/"
        report_requests.append(request)
        assert request.method == "GET"
        assert request.headers["access-token"] == "test-token"
        start = datetime.combine(report_day, datetime.min.time(), timezone(timedelta(hours=8)))
        assert request.url.params["start_time"] == str(int(start.timestamp()))
        assert request.url.params["end_time"] == str(
            int((start + timedelta(days=1) - timedelta(seconds=1)).timestamp())
        )
        if len(report_requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "err_no": 0,
                "err_msg": "private-error",
                "data": {
                    "sum": {
                        "active_user_num": 113,
                        "new_user_num": 74,
                        "total_user_num": 285227,
                        "open_time": 190,
                        "share_time": 8,
                    },
                    "behaviors": [{"private": "private-row"}],
                },
            },
        )

    reference = ConnectionRef("douyin", CONNECTION_NAME)
    credentials = Credentials("tt-test-app", SecretString("test-client-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = GetBehaviorSummaryFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"douyin-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetBehaviorSummaryInput(report_day.isoformat()))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetBehaviorSummaryOutput)
                assert output == GetBehaviorSummaryOutput(113, 74, 285227, 190, 8)
                assert len(token_requests) == 1
                assert len(report_requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
