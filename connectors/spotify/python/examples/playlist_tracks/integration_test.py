# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the Spotify Query through a real Dex Worker and Client."""

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

from dex_connectors.spotify import Client, Config, Credentials, PlaylistTrackPage, new_connection

from .flow import CONNECTION_NAME, PlaylistFlow, PlaylistInput


@pytest.mark.asyncio
async def test_playlist_query_and_unwired_failure_in_real_dex() -> None:
    """Verify provider result routing and optional branch failure through Dex."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    playlist_id = "3cEYpjA9oz9GiPac4AsH4n"
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.params.get("offset") == "1":
            return httpx.Response(404, json={})
        return httpx.Response(200, json={"limit": 2, "offset": 0, "total": 1, "next": None, "items": []})

    reference = ConnectionRef("spotify", CONNECTION_NAME)
    credentials = Credentials("", SecretString(""), SecretString("inert-test-token"), SecretString(""))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        client = Client(
            Config(endpoint="http://127.0.0.1/v1"),
            StaticCredentialProvider({reference: credentials}),
            provider_http_client,
        )
        connection = new_connection(client, reference)
        flow = PlaylistFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="spotify-python-dex-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        completed_id = f"spotify-python-{uuid4().hex}"
                        await dex_client.start_flow(flow, completed_id, PlaylistInput(playlist_id))
                        completed = await asyncio.wait_for(
                            dex_client.wait_for_flow(completed_id, timedelta(seconds=20)), 30
                        )
                        assert completed.status == dex.FlowStatus.COMPLETED, completed.error_message
                        page = completed.single_output(PlaylistTrackPage)
                        assert page.playlist_id == playlist_id and page.total == 1
                        missing_id = f"spotify-python-{uuid4().hex}"
                        await dex_client.start_flow(flow, missing_id, PlaylistInput(playlist_id, offset=1))
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
