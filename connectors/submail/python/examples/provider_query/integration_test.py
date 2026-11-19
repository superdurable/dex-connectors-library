# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise Submail retry and persistence through a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.submail as connector

from .flow import CONNECTION_NAME, ProviderQueryFlow

SUCCESS_BODY = '{"status":"success","balance":"9410","transactional_balance":"9775"}'


@pytest.mark.asyncio
async def test_provider_query_retries_503(tmp_path: Path) -> None:
    """Complete and persist the Query after a provider retry."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/balance/sms.json"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(200, content=SUCCESS_BODY.encode("utf-8"))

    reference = sdk.ConnectionRef("submail", CONNECTION_NAME)
    credentials = connector.Credentials("test-app-id", sdk.SecretString("test-app-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = connector.Client(
            connector.Config(), sdk.StaticCredentialProvider({reference: credentials}), http_client
        )
        connection = connector.new_connection(provider, reference)
        flow = ProviderQueryFlow(connection)
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
                flow_id = f"submail-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, connector.GetSMSBalanceInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(connector.SMSBalance)
                assert output.general_credits
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
