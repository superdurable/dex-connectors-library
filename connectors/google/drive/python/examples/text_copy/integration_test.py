# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run all four operations through a real Dex Worker and Client."""

import asyncio
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

from dex_connectors.google_drive import (
    Client,
    Config,
    Credentials,
    File,
    new_connection,
)

from .flow import CONNECTION_NAME, TextCopyFlow, TextCopyInput


@pytest.mark.asyncio
async def test_text_copy_uses_real_dex_worker_and_all_provider_operations() -> None:
    """Verify search, bounded read, fenced upload, and metadata read-back."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[tuple[str, str]] = []

    class Provider(BaseHTTPRequestHandler):
        def log_message(self, format: str, *args: object) -> None:
            pass  # The test records only the request method and path.

        def respond(self, status: int, body: bytes) -> None:
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self) -> None:
            requests.append(("GET", self.path))
            if self.path.startswith("/drive/v3/files?") and "appProperties" in self.path:
                self.respond(200, b'{"files":[]}')
            elif self.path.startswith("/drive/v3/files?"):
                self.respond(
                    200, b'{"files":[{"id":"source_file","name":"Ops Policy","mimeType":"text/plain"}]}'
                )
            elif self.path.startswith("/drive/v3/files/source_file?") and "alt=media" in self.path:
                self.respond(200, b"Hello")
            elif self.path.startswith("/drive/v3/files/source_file?"):
                self.respond(
                    200, b'{"id":"source_file","name":"Ops Policy","mimeType":"text/plain","size":"5"}'
                )
            elif self.path.startswith("/drive/v3/files/copied_file?"):
                self.respond(
                    200,
                    b'{"id":"copied_file","name":"Ops Policy copy.txt","mimeType":"text/plain","size":"5"}',
                )
            else:
                self.respond(404, b"{}")

        def do_POST(self) -> None:
            requests.append(("POST", self.path))
            length = int(self.headers["Content-Length"])
            body = self.rfile.read(length)
            assert b"Hello" in body
            self.respond(
                200, b'{"id":"copied_file","name":"Ops Policy copy.txt","mimeType":"text/plain","size":"5"}'
            )

    provider = ThreadingHTTPServer(("127.0.0.1", 0), Provider)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("google", CONNECTION_NAME)
    client = Client(
        Config(endpoint=f"http://127.0.0.1:{provider.server_port}"),
        StaticCredentialProvider({reference: Credentials(access_token=SecretString("inert-token"))}),
    )
    connection = new_connection(client, reference)
    try:
        with tempfile.TemporaryDirectory(prefix="google-drive-python-dex-blobs-") as cache_directory:
            flow = TextCopyFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        flow_id = f"google-drive-python-text-copy-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow, flow_id, TextCopyInput("Ops Policy", "Ops Policy copy.txt")
                        )
                        result = await asyncio.wait_for(
                            dex_client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        copy = result.single_output(File)
                        assert copy.id == "copied_file"
                        assert copy.name == "Ops Policy copy.txt"
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
    assert len(requests) == 6
    assert [method for method, _ in requests] == ["GET", "GET", "GET", "GET", "POST", "GET"]
