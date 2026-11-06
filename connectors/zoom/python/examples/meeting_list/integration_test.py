# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the Zoom meeting-list example through a real Dex Worker and Client."""

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

from dex_connectors.zoom import Client, Config, Credentials, ListMeetingsInput, MeetingPage, new_connection

from .flow import CONNECTION_NAME, MeetingListFlow


@pytest.mark.asyncio
async def test_meeting_list_retries_and_fails_unwired_rejection_in_real_dex() -> None:
    """Check Query retry and optional-branch failure across Worker persistence."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.params.get("type") == "live":
            return httpx.Response(403, json={"code": 200, "message": "Denied"})
        if len(requests) == 1:
            return httpx.Response(503, json={"code": 503, "message": "Unavailable"})
        return httpx.Response(
            200,
            json={
                "total_records": 1,
                "meetings": [
                    {
                        "id": 92674392836,
                        "topic": "Consultation",
                        "type": 2,
                        "start_time": "2036-02-24T17:00:00Z",
                        "duration": 30,
                        "timezone": "America/Los_Angeles",
                    }
                ],
            },
        )

    reference = ConnectionRef("zoom", CONNECTION_NAME)
    credentials = Credentials(
        "zoom-client-id",
        SecretString("zoom-client-secret"),
        SecretString("zoom-test-access-token"),
        SecretString("zoom-refresh-token"),
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider:
        client = Client(
            Config(endpoint="https://zoom.example/v2"),
            StaticCredentialProvider({reference: credentials}),
            provider,
        )
        connection = new_connection(client, reference)
        flow = MeetingListFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="zoom-python-dex-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        flow_id = f"zoom-python-list-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow, flow_id, ListMeetingsInput(type="upcoming", page_size=2)
                        )
                        result = await asyncio.wait_for(
                            dex_client.wait_for_flow(flow_id, timedelta(seconds=20)), 30
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        page = result.single_output(MeetingPage)
                        assert page.total_records == 1
                        assert page.meetings[0].id == 92674392836
                        assert len(requests) == 2
                        rejected_id = f"zoom-python-list-{uuid4().hex}"
                        await dex_client.start_flow(flow, rejected_id, ListMeetingsInput(type="live"))
                        rejected = await asyncio.wait_for(
                            dex_client.wait_for_flow(rejected_id, timedelta(seconds=20)), 30
                        )
                        assert rejected.status == dex.FlowStatus.FAILED
                        assert "providerRejected" in (rejected.error_message or "")
                finally:
                    await worker.stop()
                    await task
            finally:
                cache.close()
