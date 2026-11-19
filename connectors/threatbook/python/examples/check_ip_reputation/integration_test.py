# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the threatbook Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.threatbook import (
    CheckIPReputationInput,
    CheckIPReputationOutput,
    Client,
    Config,
    Credentials,
    new_connection,
)

from .flow import CONNECTION_NAME, CheckIPReputationFlow


@pytest.mark.asyncio
async def test_check_ip_reputation_through_dex(tmp_path: Path) -> None:
    """Run the checked-in Flow across Worker, Client, and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    responses = [
        (
            200,
            '{"response_code":0,"verbose_msg":"Ok","data":{"159.203.93.255":{"is_malicious":true,"confidence_level":"high","severity":"medium","judgments":["Scanner"],"permalink":"https://example.invalid/reputation"}}}',
        )
    ]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = responses[len(requests) - 1]
        return httpx.Response(status, content=body.encode())

    reference = ConnectionRef("threatbook", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider_client, reference)
        flow = CheckIPReputationFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"threatbook-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, CheckIPReputationInput(ip_address="159.203.93.255"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(CheckIPReputationOutput)
                assert output.confidence_level == "high"
                assert len(requests) == 1
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
