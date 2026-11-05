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

from dex_connectors.mysql import RowSet
from dex_connectors.mysql.mysqlmock import (
    Mock,
    execute_completed_default_answer,
    query_completed,
    query_retry,
)

from .flow import CONNECTION_NAME, RecordRefundFlow, RefundOutcome, RefundRequest


@pytest.mark.asyncio
async def test_record_refund_with_real_dex(tmp_path: Path) -> None:
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    mock = Mock(CONNECTION_NAME)
    mock.query_script.default(query_completed(RowSet()))
    flow = RecordRefundFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 64 * 1024 * 1024))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as client:
            flow_id = "mysql-python-" + uuid4().hex
            mock.query_script.for_flow(flow_id).respond(
                query_retry(Failure(FailureKind.AVAILABILITY, "mysql", "query", "temporary outage")),
                query_completed(RowSet()),
            )
            mock.execute_script.for_flow(flow_id).respond(execute_completed_default_answer())
            await client.start_flow(flow, flow_id, RefundRequest("88213", "250.00"))
            result = await client.wait_for_flow(flow_id, timedelta(seconds=30))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            assert result.single_output(RefundOutcome).status == "recorded"
            query_calls = [call for call in mock.query_script.calls() if call.flow_id == flow_id]
            assert len(query_calls) == 2
            assert query_calls[0].call_id == query_calls[1].call_id
    finally:
        await worker.stop()
        await task
        cache.close()
