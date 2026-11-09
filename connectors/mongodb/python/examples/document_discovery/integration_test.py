# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification of the MongoDB Python example."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.mongodb import DocumentSet, FindDocumentsInput
from dex_connectors.mongodb.mongodbmock import Mock

from .flow import DocumentDiscoveryFlow


@pytest.mark.asyncio
async def test_document_discovery_uses_default_mock_with_real_dex(tmp_path: Path) -> None:
    """Run the checked-in Flow across Worker, Client, and Attribute boundaries."""
    mock = Mock()
    flow = DocumentDiscoveryFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=15)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            flow_id = f"mongodb-python-{uuid4().hex}"
            await client.start_flow(flow, flow_id, FindDocumentsInput("orders", {"status": "active"}))
            result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            output = result.single_output(DocumentSet)
            assert len(output.documents) == 1
            assert output.documents[0] is not None and output.documents[0]["status"] == "active"
            assert len(mock.find_documents_script.calls()) == 1
    finally:
        await worker.stop()
        await worker_task
        cache.close()
