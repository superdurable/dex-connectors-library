# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the checked-in example."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.microsoft_outlook_calendar import (
    Client,
    Config,
    Credentials,
    Event,
    GetEventInput,
    new_connection,
)

from .flow import CONNECTION_NAME, ReadOutlookEventFlow


async def _run_one_flow(tmp_path: Path, response: httpx.Response) -> dex.FlowResult:
    reference = ConnectionRef("microsoft", CONNECTION_NAME)
    credentials = StaticCredentialProvider(
        {reference: Credentials(auth_method_id="microsoft-oauth", access_token=SecretString("inert-access"))}
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _request: response)) as http_client:
        connector_client = Client(
            Config(), credentials, http_client, local_provider_url="http://127.0.0.1:12345"
        )
        flow = ReadOutlookEventFlow(new_connection(connector_client, reference))
        blob_cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 64 * 1024 * 1024))
        registry = dex.Registry([flow], allow_async_handlers=True)
        address = os.environ.get("DEX_SERVER_ADDRESS", "localhost:40720")
        worker = dex.AsyncWorker(registry, blob_cache, dex.WorkerOptions(server_address=address))
        client = dex.AsyncClient(registry, blob_cache, dex.ClientOptions(server_address=address))
        task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            flow_id = "outlook-read-" + uuid4().hex
            await client.start_flow(flow, flow_id, GetEventInput(event_id="event-1"))
            return await client.wait_for_flow(flow_id, timeout=timedelta(seconds=40))
        finally:
            await client.close()
            await worker.close()
            await task
            blob_cache.close()


@pytest.mark.asyncio
async def test_real_dex_read_completes(tmp_path: Path) -> None:
    """Dex executes the generated Query Step and returns the Graph event."""
    result = await _run_one_flow(
        tmp_path,
        httpx.Response(
            200,
            json={
                "id": "event-1",
                "subject": "Planning",
                "showAs": "busy",
                "start": {"dateTime": "2026-02-24T17:00:00", "timeZone": "UTC"},
                "end": {"dateTime": "2026-02-24T18:00:00", "timeZone": "UTC"},
            },
        ),
    )
    assert result.status == dex.FlowStatus.COMPLETED
    assert result.single_output(Event).id == "event-1"


@pytest.mark.asyncio
async def test_real_dex_unwired_not_found_fails(tmp_path: Path) -> None:
    """An unwired optional notFound branch fails the real Dex Flow."""
    result = await _run_one_flow(tmp_path, httpx.Response(404, json={}))
    assert result.status == dex.FlowStatus.FAILED
    assert result.error_message is not None
    assert "notFound" in result.error_message
