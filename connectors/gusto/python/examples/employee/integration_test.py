# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Gusto Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.gusto import (
    Client,
    Config,
    Credentials,
    Employee,
    GetEmployeeInput,
    new_connection,
)

from .flow import CONNECTION_NAME, EmployeeFlow


@pytest.mark.asyncio
async def test_employee_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Bearer synthetic-gusto-token"
        assert request.url.path == "/v1/employees/90a68db5-cb3a-4f64-bb60-c0a934ee7600"
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "uuid": "90a68db5-cb3a-4f64-bb60-c0a934ee7600",
                "first_name": "Ada",
                "last_name": "Lovelace",
                "email": "ada@example.com",
                "company_uuid": "7d61f73e-a18c-46a5-88e2-2bfa894657d7",
            },
        )

    reference = ConnectionRef("gusto", CONNECTION_NAME)
    credentials = Credentials(
        "synthetic-client",
        SecretString("synthetic-secret"),
        SecretString("synthetic-gusto-token"),
        SecretString("synthetic-refresh"),
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(redirect_uri="https://dex.example/callback", base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = EmployeeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"gusto-python-{uuid4().hex}"
                await client.start_flow(
                    flow, flow_id, GetEmployeeInput("90a68db5-cb3a-4f64-bb60-c0a934ee7600")
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(Employee).employee_id == "90a68db5-cb3a-4f64-bb60-c0a934ee7600"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
