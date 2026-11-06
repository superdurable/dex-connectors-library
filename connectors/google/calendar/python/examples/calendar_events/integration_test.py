# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the Calendar Query with a real Dex Worker and Client."""

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

from dex_connectors.google_calendar import Client, Config, Credentials, ListEventsOutput, new_connection

from .flow import CONNECTION_NAME, CalendarEventsFlow, CalendarWindowInput


@pytest.mark.asyncio
async def test_calendar_query_and_optional_branch_in_real_dex() -> None:
    """Verify success and unwired failure routing through a real Dex stack."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.params.get("timeMin") == "2026-02-25T00:00:00Z":
            return httpx.Response(404, json={})
        return httpx.Response(200, json={"timeZone": "America/Los_Angeles", "items": []})

    reference = ConnectionRef("google", CONNECTION_NAME)
    credentials = Credentials(access_token=SecretString("inert-test-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        client = Client(
            Config(endpoint="http://127.0.0.1/calendar/v3"),
            StaticCredentialProvider({reference: credentials}),
            provider_http_client,
        )
        flow = CalendarEventsFlow(new_connection(client, reference))
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="google-calendar-python-dex-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        flow_id = f"google-calendar-python-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow,
                            flow_id,
                            CalendarWindowInput("primary", "2026-02-24T00:00:00Z", "2026-02-25T00:00:00Z"),
                        )
                        completed = await asyncio.wait_for(
                            dex_client.wait_for_flow(flow_id, timedelta(seconds=20)), 30
                        )
                        assert completed.status == dex.FlowStatus.COMPLETED, completed.error_message
                        assert completed.single_output(ListEventsOutput).calendar_id == "primary"
                        missing_id = f"google-calendar-python-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow,
                            missing_id,
                            CalendarWindowInput("primary", "2026-02-25T00:00:00Z", "2026-02-26T00:00:00Z"),
                        )
                        missing = await asyncio.wait_for(
                            dex_client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                        )
                        assert missing.status == dex.FlowStatus.FAILED
                        assert "notFound" in (missing.error_message or "")
                        assert len(requests) == 2
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
