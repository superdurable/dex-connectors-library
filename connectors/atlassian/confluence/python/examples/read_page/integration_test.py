# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the checked-in Flow through a real Dex Worker and Client."""

import asyncio
import json
import os
import tempfile
import threading
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.atlassian_confluence import Client, Config, Credentials, Page, new_connection

from .flow import CONNECTION_NAME, ReadPageFlow, ReadPageRequest

_CLOUD_ID = "11223344-a1b2-4b33-8c44-def123456789"


class _ConfluenceStandIn(BaseHTTPRequestHandler):
    requests: list[str] = []

    def do_GET(self) -> None:
        """Answer one page read or a conclusive missing-page response."""
        self.requests.append(self.path)
        if self.path.endswith("/pages/699999?body-format=storage"):
            status, body = 404, b"{}"
        else:
            status = 200
            body = json.dumps(
                {
                    "id": "600002",
                    "title": "Remote work policy",
                    "status": "current",
                    "spaceId": "98306",
                    "createdAt": "2026-10-25T09:00:00Z",
                    "version": {"number": 1, "createdAt": "2026-10-25T09:00:00Z"},
                    "body": {"storage": {"value": "<p>Hello</p>"}},
                }
            ).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format: str, *args: object) -> None:
        """Keep synthetic provider traffic out of test logs."""


@pytest.mark.asyncio
async def test_read_page_flow_uses_real_dex_worker_and_client() -> None:
    """Verify successful persistence and an unwired optional branch failure."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    _ConfluenceStandIn.requests = []
    server = ThreadingHTTPServer(("127.0.0.1", 0), _ConfluenceStandIn)
    server_thread = threading.Thread(target=server.serve_forever, daemon=True)
    server_thread.start()
    provider_endpoint = f"http://127.0.0.1:{server.server_address[1]}"
    reference = ConnectionRef("atlassian", CONNECTION_NAME)
    credentials = Credentials(
        "inert-client",
        SecretString("inert-secret"),
        SecretString("inert-access-token"),
        SecretString("inert-refresh-token"),
    )
    provider = StaticCredentialProvider({reference: credentials})
    client = Client(Config(cloud_id=_CLOUD_ID, endpoint=provider_endpoint), provider)
    connection = new_connection(client, reference)
    with tempfile.TemporaryDirectory(prefix="confluence-python-dex-") as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
        flow = ReadPageFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=address)
            ) as dex_client:
                found_id = f"confluence-read-{uuid4().hex}"
                await dex_client.start_flow(flow, found_id, ReadPageRequest("600002"))
                found = await asyncio.wait_for(dex_client.wait_for_flow(found_id, timedelta(seconds=20)), 30)
                assert found.status == dex.FlowStatus.COMPLETED, found.error_message
                assert found.single_output(Page).body == "Hello"
                missing_id = f"confluence-read-{uuid4().hex}"
                await dex_client.start_flow(flow, missing_id, ReadPageRequest("699999"))
                missing = await asyncio.wait_for(
                    dex_client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                )
                assert missing.status == dex.FlowStatus.FAILED
                assert "notFound" in (missing.error_message or "")
                assert len(_ConfluenceStandIn.requests) == 2
        finally:
            await worker.stop()
            await task
            cache.close()
            await connection.aclose()
            server.shutdown()
            server.server_close()
            server_thread.join(timeout=5)
