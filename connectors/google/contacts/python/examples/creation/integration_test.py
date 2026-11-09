# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for contact creation with a mock connection."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.google_contacts import Contact, CreateContactInput
from dex_connectors.google_contacts.contactsmock import Mock

from .flow import CreationFlow


@pytest.mark.asyncio
async def test_create_contact_with_mock_on_real_dex(tmp_path: Path) -> None:
    """Execute the checked-in creation Flow through real Dex persistence."""
    mock = Mock()
    flow = CreationFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=15)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            flow_id = f"google-contacts-create-python-{uuid4().hex}"
            await client.start_flow(
                flow, flow_id, CreateContactInput("Example", "Contact", "example@example.com")
            )
            result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            assert result.single_output(Contact).resource_name == "people/c123"
    finally:
        await worker.stop()
        await worker_task
        cache.close()
