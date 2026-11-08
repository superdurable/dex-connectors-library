# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Razorpay Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.razorpay import (
    Client,
    Config,
    Credentials,
    GetPaymentInput,
    PaymentStatus,
    new_connection,
)

from .flow import CONNECTION_NAME, PaymentInspectionFlow


@pytest.mark.asyncio
async def test_payment_inspection_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/api/payments/pay_DG4ZdRK8ZnXC3k"
        assert request.headers["Authorization"] == (
            "Basic cnpwX3Rlc3RfMTIzNDU2Nzg5MDpzYW1wbGUtcmF6b3JwYXktc2VjcmV0"
        )
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "id": "pay_DG4ZdRK8ZnXC3k",
                "entity": "payment",
                "status": "captured",
                "amount": 100,
                "currency": "INR",
            },
        )

    reference = ConnectionRef("razorpay", CONNECTION_NAME)
    credentials = Credentials(SecretString("rzp_test_1234567890"), SecretString("sample-razorpay-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345/api"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = PaymentInspectionFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"razorpay-python-{uuid4().hex}"
                await client.start_flow(
                    flow, flow_id, GetPaymentInput(payment_id="pay_DG4ZdRK8ZnXC3k")
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(PaymentStatus).id == "pay_DG4ZdRK8ZnXC3k"
                assert result.single_output(PaymentStatus).status == "captured"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
