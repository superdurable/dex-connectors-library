# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the search Flow across a real Dex Worker and Client."""

import asyncio
import json
import os
import tempfile
import threading
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.google_youtube import Client, Config, Credentials, VideoSearchPage, new_connection

from .flow import CONNECTION_NAME, SearchInput, VideoSearchFlow

CHANNEL_ID = "UC_x5XG1OV2P6uZZ5FSM9Ttw"
VIDEO_ID = "dQw4w9WgXcQ"


class ProviderHandler(BaseHTTPRequestHandler):
    """Return one public search page or a definite quota error."""

    def do_GET(self) -> None:
        """Answer the bounded search request without external credentials."""
        body: dict[str, Any]
        if "quota" in self.path:
            self.send_response(403)
            body = {"error": {"errors": [{"reason": "quotaExceeded"}]}}
        else:
            self.send_response(200)
            body = {
                "pageInfo": {"totalResults": 1},
                "items": [
                    {
                        "id": {"kind": "youtube#video", "videoId": VIDEO_ID},
                        "snippet": {
                            "channelId": CHANNEL_ID,
                            "channelTitle": "Compiler Lab",
                            "title": "Compiler walkthrough",
                            "description": "A tour",
                            "publishedAt": "2026-10-01T16:00:00Z",
                        },
                    }
                ],
            }
        payload = json.dumps(body).encode()
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, format: str, *args: object) -> None:
        """Keep the test provider quiet."""


@pytest.mark.asyncio
async def test_search_and_unwired_quota_branch_in_real_dex() -> None:
    """Verify completion, persisted output, and optional branch failure."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    provider = ThreadingHTTPServer(("127.0.0.1", 0), ProviderHandler)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("google", CONNECTION_NAME)
    credentials = Credentials(
        "google-oauth-readonly",
        "inert-client",
        SecretString("inert-secret"),
        SecretString("youtube-token"),
        SecretString("inert-refresh"),
    )
    client = Client(
        Config(endpoint=f"http://127.0.0.1:{provider.server_address[1]}/youtube/v3"),
        StaticCredentialProvider({reference: credentials}),
    )
    connection = new_connection(client, reference)
    flow = VideoSearchFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    try:
        with tempfile.TemporaryDirectory(prefix="youtube-python-dex-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        completed_id = f"youtube-python-{uuid4().hex}"
                        await dex_client.start_flow(flow, completed_id, SearchInput("compiler"))
                        completed = await asyncio.wait_for(
                            dex_client.wait_for_flow(completed_id, timedelta(seconds=20)), 30
                        )
                        assert completed.status == dex.FlowStatus.COMPLETED, completed.error_message
                        page = completed.single_output(VideoSearchPage)
                        assert page.results[0].video_id == VIDEO_ID
                        quota_id = f"youtube-python-{uuid4().hex}"
                        await dex_client.start_flow(flow, quota_id, SearchInput("quota"))
                        failed = await asyncio.wait_for(
                            dex_client.wait_for_flow(quota_id, timedelta(seconds=20)), 30
                        )
                        assert failed.status == dex.FlowStatus.FAILED
                        assert "quotaExhausted" in (failed.error_message or "")
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
    finally:
        await connection.aclose()
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
