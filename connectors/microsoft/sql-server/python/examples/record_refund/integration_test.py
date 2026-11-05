# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the checked-in refund Flow through a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import Failure, FailureKind

from dex_connectors.microsoft_sql_server.sqlservermock import (
    Mock,
    execute_completed_default_answer,
    execute_provider_rejected,
    execute_retry,
)

from .flow import CONNECTION_NAME, RecordRefundFlow, RefundRequest


@pytest.mark.asyncio
async def test_refund_flow_routes_success_retry_and_unwired_rejection(tmp_path: Path) -> None:
    """Check durable operation routing and stable retry identity in Dex."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    mock = Mock(CONNECTION_NAME)
    flow = RecordRefundFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 64 * 1024 * 1024))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            success_id = f"sqlserver-python-success-{uuid4().hex}"
            await client.start_flow(flow, success_id, RefundRequest("order-88213", "250.00"))
            success = await client.wait_for_flow(success_id, timedelta(seconds=30))
            assert success.status == dex.FlowStatus.COMPLETED, success.error_message

            retry_id = f"sqlserver-python-retry-{uuid4().hex}"
            mock.execute_script.for_flow(retry_id).respond(
                execute_retry(
                    Failure(FailureKind.AVAILABILITY, "microsoft-sql-server", "execute", "temporary outage")
                ),
                execute_completed_default_answer(),
            )
            await client.start_flow(flow, retry_id, RefundRequest("order-88214", "100.00"))
            retried = await client.wait_for_flow(retry_id, timedelta(seconds=30))
            assert retried.status == dex.FlowStatus.COMPLETED, retried.error_message
            calls = [item for item in mock.execute_script.calls() if item.flow_id == retry_id]
            assert len(calls) == 2
            assert calls[0].call_id == calls[1].call_id

            rejected_id = f"sqlserver-python-rejected-{uuid4().hex}"
            mock.execute_script.for_flow(rejected_id).respond(
                execute_provider_rejected(
                    value=execute_completed_default_answer().value,
                    failure=Failure(FailureKind.CONFLICT, "microsoft-sql-server", "execute", "duplicate key"),
                )
            )
            await client.start_flow(flow, rejected_id, RefundRequest("order-88215", "100.00"))
            rejected = await client.wait_for_flow(rejected_id, timedelta(seconds=30))
            assert rejected.status == dex.FlowStatus.FAILED
            assert rejected.error_message is not None and "providerRejected" in rejected.error_message
    finally:
        await worker.stop()
        await worker_task
        cache.close()
