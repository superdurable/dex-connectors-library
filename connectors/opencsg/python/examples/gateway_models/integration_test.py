# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify opencsg retry through a real Dex Worker and Client."""

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

from dex_connectors.opencsg import (
    Client,
    Config,
    Credentials,
    ListGatewayModelsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, GatewayCatalogRequest, ListGatewayModelsFlow


@pytest.mark.asyncio
async def test_provider_query_retries_503(tmp_path: Path) -> None:
    """Persist the Go-recorded result after one provider availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    scenarios = json.loads(
        (Path(__file__).parents[5] / "conformance/connectors/opencsg/scenarios.json").read_text()
    )["scenarios"]
    success = scenarios[0]
    exchanges = success["exchanges"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        if len(requests) == 3:
            echo = next(case for case in scenarios if case["name"] == "listGatewayModels-escapedKey")
            return httpx.Response(200, content=echo["exchanges"][0]["body"])
        exchange = exchanges[len(requests) - 2]
        assert request.method == exchange["method"]
        assert request.url.path == exchange["path"]
        return httpx.Response(exchange["status"], content=exchange["body"])

    reference = ConnectionRef("opencsg", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = ListGatewayModelsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"opencsg-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GatewayCatalogRequest())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=30)), 35)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListGatewayModelsOutput)
                assert output.model_ids == tuple(success["expected"]["output"]["modelIDs"])
                assert len(requests) == len(exchanges) + 1
                echo_id = f"opencsg-python-echo-{uuid4().hex}"
                await client.start_flow(flow, echo_id, GatewayCatalogRequest())
                failed = await asyncio.wait_for(client.wait_for_flow(echo_id, timedelta(seconds=30)), 35)
                assert failed.status == dex.FlowStatus.FAILED
                assert failed.error_message is not None
                assert "invalidResponse" in failed.error_message
                assert "test-secret" not in failed.error_message
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
