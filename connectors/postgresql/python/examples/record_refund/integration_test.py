# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the checked-in refund Flow on a real local Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.postgresql import RowSet
from dex_connectors.postgresql.postgresqlmock import Mock, query_completed

from .flow import CONNECTION_NAME, RecordRefundFlow, RefundOutcome, RefundRequest


@pytest.mark.asyncio
async def test_record_refund_real_dex_worker(tmp_path: Path) -> None:
    """Check both PostgreSQL Steps and existing-row short circuit on Dex."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    mock = Mock(CONNECTION_NAME)
    flow = RecordRefundFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 64 * 1024 * 1024))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as client:
            flow_id = f"postgresql-python-record-{uuid4()}"
            request = RefundRequest("order-88213", "250.00", "SUP-88213")
            await client.start_flow(flow, flow_id, request)
            completed = await client.wait_for_flow(flow_id, timedelta(seconds=30))
            assert completed.status == dex.FlowStatus.COMPLETED
            outputs = [
                item.decode(RefundOutcome)
                for item in completed.completions
                if item.step_type == "CompleteRefundRecording"
            ]
            assert len(outputs) == 1 and outputs[0].status == "recorded"
            assert outputs[0].refund["order_id"] == request.order_id
            query_calls = [call for call in mock.query_script.calls() if call.flow_id == flow_id]
            execute_calls = [call for call in mock.execute_script.calls() if call.flow_id == flow_id]
            assert len(query_calls) == len(execute_calls) == 1
            assert execute_calls[0].input.idempotency_key_placeholder == 4

            existing_id = f"postgresql-python-existing-{uuid4()}"
            mock.query_script.for_flow(existing_id).respond(
                query_completed(RowSet(rows=({"order_id": "existing-order"},)))
            )
            await client.start_flow(flow, existing_id, RefundRequest("existing-order", "1.00"))
            existing = await client.wait_for_flow(existing_id, timedelta(seconds=30))
            assert existing.status == dex.FlowStatus.COMPLETED
            outputs = [
                item.decode(RefundOutcome)
                for item in existing.completions
                if item.step_type == "DecideRefundRecording"
            ]
            assert len(outputs) == 1 and outputs[0].status == "alreadyRecorded"
            assert not any(call.flow_id == existing_id for call in mock.execute_script.calls())
    finally:
        await worker.stop()
        await worker_task
        cache.close()
