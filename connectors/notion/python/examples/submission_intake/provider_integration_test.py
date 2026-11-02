# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the example through real Dex and a local Notion HTTP stand-in."""

import asyncio
import json
import os
import threading
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.notion import Client, Config, Credentials, GetPageOutput, new_connection

from .flow import CONNECTION_NAME, SubmissionFlow, SubmissionRequest

PAGE_ID = "2a4d6f80-1b3c-4d5e-8f70-112233445577"
SOURCE_ID = "2a4d6f80-1b3c-4d5e-8f70-112233445566"
PAGE = {
    "object": "page",
    "id": PAGE_ID,
    "url": "https://www.notion.so/2a4d6f801b3c4d5e8f70112233445577",
    "parent": {"type": "data_source_id", "data_source_id": SOURCE_ID},
    "properties": {"Name": {"id": "title", "type": "title", "title": [{"plain_text": "Ada Lovelace"}]}},
}


class NotionStandIn(BaseHTTPRequestHandler):
    """Return bounded Notion shapes and retain secret-safe request facts."""

    server: "NotionServer"

    def log_message(self, format: str, *args: object) -> None:
        """Keep test logs free of Authorization headers."""

    def do_GET(self) -> None:
        """Serve page reads."""
        self._answer()

    def do_POST(self) -> None:
        """Serve search, query, and create requests."""
        self._answer()

    def do_PATCH(self) -> None:
        """Serve absolute page updates."""
        self._answer()

    def _answer(self) -> None:
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        self.server.requests.append((self.command, self.path, body))
        assert self.headers.get("Authorization") == "Bearer ntn_test_token_value"
        assert self.headers.get("Notion-Version") == "2026-03-11"
        assert self.headers.get("Idempotency-Key") is None
        status = 200
        if self.path == "/v1/search":
            query = json.loads(body).get("query", "")
            if query.startswith("rejected"):
                status, output = 400, {"object": "error", "code": "validation_error", "message": "SENTINEL"}
            elif (
                query.startswith("retry")
                and sum(
                    1
                    for method, path, previous in self.server.requests
                    if method == "POST"
                    and path == "/v1/search"
                    and json.loads(previous).get("query") == query
                )
                == 1
            ):
                status, output = 429, {"object": "error", "code": "rate_limited", "message": "SENTINEL"}
            else:
                output = {
                    "results": [
                        {
                            "object": "data_source",
                            "id": SOURCE_ID,
                            "title": [{"plain_text": "Ada Lovelace"}],
                            "parent": {"type": "database_id", "database_id": SOURCE_ID},
                        }
                    ],
                    "next_cursor": None,
                    "has_more": False,
                }
        elif self.path.endswith("/query"):
            output = {"results": [], "next_cursor": None, "has_more": False}
        elif self.command == "POST" and self.path == "/v1/pages":
            output = PAGE
        elif self.command == "PATCH" and self.path == "/v1/pages/" + PAGE_ID:
            output = PAGE
        elif self.command == "GET" and self.path == "/v1/pages/" + PAGE_ID:
            output = PAGE
        else:
            status, output = 404, {"object": "error", "code": "object_not_found", "message": "SENTINEL"}
        encoded = json.dumps(output).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)


class NotionServer(ThreadingHTTPServer):
    """Bind the provider stand-in to an OS-selected free loopback port."""

    requests: list[tuple[str, str, bytes]]


@pytest.mark.asyncio
async def test_real_dex_routes_all_provider_operations(tmp_path: Path) -> None:
    """Verify live Step execution, safe retry, and unwired branch failure."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    provider = NotionServer(("127.0.0.1", 0), NotionStandIn)
    provider.requests = []
    thread = threading.Thread(target=provider.serve_forever, daemon=True)
    thread.start()
    reference = ConnectionRef("notion", CONNECTION_NAME)
    connector_client = Client(
        Config(endpoint=f"http://127.0.0.1:{provider.server_address[1]}"),
        StaticCredentialProvider({reference: Credentials(SecretString("ntn_test_token_value"))}),
    )
    connection = new_connection(connector_client, reference)
    flow = SubmissionFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 25))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            for title in ("Ada Lovelace", "retry Ada", "rejected Ada"):
                flow_id = f"notion-python-http-{uuid4().hex}"
                request = SubmissionRequest(SOURCE_ID, PAGE_ID, title, "ada@example.com")
                await client.start_flow(flow, flow_id, request)
                result = await client.wait_for_flow(flow_id, timedelta(seconds=30))
                if title.startswith("rejected"):
                    assert result.status == dex.FlowStatus.FAILED
                    assert result.error_message is not None and "providerRejected" in result.error_message
                else:
                    assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                    assert result.single_output(GetPageOutput).page.id == PAGE_ID
        methods = [method for method, _, _ in provider.requests]
        assert methods.count("POST") == 8
        assert methods.count("PATCH") == 2
        assert methods.count("GET") == 2
        assert b"SENTINEL" not in repr(provider.requests).encode()
    finally:
        await worker.stop()
        await task
        cache.close()
        await connection.aclose()
        provider.shutdown()
        provider.server_close()
        thread.join(timeout=5)
