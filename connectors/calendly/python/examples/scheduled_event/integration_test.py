# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the scheduled event example through a real Dex Worker and Client."""

import asyncio
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.calendly import (
    Client,
    Config,
    Credentials,
    GetScheduledEventInput,
    ScheduledEvent,
    new_connection,
)

from .flow import CONNECTION_NAME, ScheduledEventFlow

EVENT_URI = "https://api.calendly.com/scheduled_events/EVT00001"
CONNECTION = ConnectionRef("calendly", CONNECTION_NAME)


@pytest.mark.asyncio
async def test_lookup_retries_and_fails_unwired_rejection_in_real_dex() -> None:
    """Verify a retry, successful persistence, and optional branch failure."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("EVT404"):
            return httpx.Response(404, json={"title": "Not found"})
        if len(requests) == 1:
            return httpx.Response(503, json={"title": "Unavailable"})
        return httpx.Response(
            200,
            json={
                "resource": {
                    "uri": EVENT_URI,
                    "name": "Discovery Call",
                    "status": "active",
                    "start_time": "2026-11-02T17:00:00Z",
                    "end_time": "2026-11-02T17:30:00Z",
                }
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http:
        client = Client(
            Config(),
            StaticCredentialProvider({CONNECTION: Credentials(access_token=SecretString("inert-token"))}),
            provider_http,
        )
        connection = new_connection(client, CONNECTION)
        flow = ScheduledEventFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="calendly-python-dex-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=15)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=server_address)
                ) as dex_client:
                    success_id = f"calendly-python-event-{uuid4().hex}"
                    await dex_client.start_flow(flow, success_id, GetScheduledEventInput(EVENT_URI))
                    success = await asyncio.wait_for(
                        dex_client.wait_for_flow(success_id, timedelta(seconds=20)), 30
                    )
                    assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                    assert success.single_output(ScheduledEvent).uri == EVENT_URI
                    assert len(requests) == 2
                    rejected_id = f"calendly-python-event-{uuid4().hex}"
                    await dex_client.start_flow(
                        flow,
                        rejected_id,
                        GetScheduledEventInput("https://api.calendly.com/scheduled_events/EVT404"),
                    )
                    rejected = await asyncio.wait_for(
                        dex_client.wait_for_flow(rejected_id, timedelta(seconds=20)), 30
                    )
                    assert rejected.status == dex.FlowStatus.FAILED
                    assert rejected.error_message is not None and "notFound" in rejected.error_message
            finally:
                await worker.stop()
                await worker_task
                cache.close()
