# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise all Hiver operations through a real Dex Worker and Client."""

import asyncio
import json
import os
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Thread
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.hiver import Client, Config, Credentials, new_connection

from .flow import CONNECTION_NAME, ClaimConversationFlow, ClaimOutcome, ClaimRequest


@pytest.mark.asyncio
async def test_claim_conversation_real_dex_stack(tmp_path: Path) -> None:
    """Verify six provider Steps, a durable retry, and both multipart writes."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    calls: list[tuple[str, str, bytes]] = []
    inbox_page = {
        "data": {
            "results": [{"id": 105902, "display_name": "Support", "email": "support@example.com"}],
            "pagination": {"next_page": None},
        }
    }
    conversation = {
        "id": 573741352,
        "status": "open",
        "message_ids": [{"hiver_message_id": 834466048}],
    }

    class HiverHandler(BaseHTTPRequestHandler):
        def _write(self, status: int, value: object | None = None) -> None:
            body = b"" if value is None else json.dumps(value, separators=(",", ":")).encode()
            self.send_response(status)
            if body:
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            if body:
                self.wfile.write(body)

        def _record(self) -> str:
            length = int(self.headers.get("Content-Length", "0"))
            body = self.rfile.read(length)
            path = self.path.split("?", 1)[0]
            calls.append((self.command, self.path, body))
            return path

        def do_GET(self) -> None:
            path = self._record()
            if path == "/v1/inboxes":
                if len(calls) == 1:
                    self._write(429)
                else:
                    self._write(200, inbox_page)
            elif path.endswith("/conversations"):
                self._write(200, {"data": {"results": [conversation], "pagination": {"next_page": None}}})
            elif path.endswith("/conversations/573741352"):
                current = dict(conversation)
                if any(method == "PATCH" for method, _, _ in calls):
                    current["status"] = "pending"
                self._write(200, {"data": [current]})
            else:
                self._write(404)

        def do_PATCH(self) -> None:
            path = self._record()
            assert path.endswith("/conversations/573741352")
            self._write(204)

        def do_POST(self) -> None:
            path = self._record()
            if path.endswith("/notes"):
                self._write(201, {"data": {"id": 35042650, "created_at": "2026-11-03T15:00:00Z"}})
            elif path.endswith("/shared-drafts"):
                self._write(201, {"data": {"id": 573741352, "shared_draft_id": 901}})
            else:
                self._write(404)

        def log_message(self, format: str, *args: object) -> None:
            pass  # The stand-in never logs request bodies or credentials.

    provider = ThreadingHTTPServer(("127.0.0.1", 0), HiverHandler)
    provider_thread = Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("hiver", CONNECTION_NAME)
    client = Client(
        Config(request_interval_milliseconds=1),
        StaticCredentialProvider({reference: Credentials(SecretString("inertHiverTestKey012345"))}),
        api_base_url=f"http://127.0.0.1:{provider.server_port}/v1",
    )
    connection = new_connection(client, reference)
    flow = ClaimConversationFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as dex_client:
            flow_id = f"hiver-python-claim-{uuid4().hex}"
            request = ClaimRequest(
                "105902",
                "573741352",
                "834466048",
                "I am reviewing this conversation.",
                "Thank you for contacting us.",
            )
            await dex_client.start_flow(flow, flow_id, request)
            result = await asyncio.wait_for(dex_client.wait_for_flow(flow_id, timedelta(seconds=40)), 50)
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            assert result.single_output(ClaimOutcome) == ClaimOutcome(
                "573741352", "pending", "35042650", "901"
            )
        assert len(calls) == 8
        assert sum(method == "GET" and path.startswith("/v1/inboxes?") for method, path, _ in calls) == 2
        assert [method for method, _, _ in calls].count("PATCH") == 1
        note_body = next(body for method, path, body in calls if method == "POST" and path.endswith("/notes"))
        draft_body = next(
            body for method, path, body in calls if method == "POST" and path.endswith("/shared-drafts")
        )
        assert b'name="content"' in note_body
        assert b'name="body"' in draft_body and b'name="hiver_message_id"' in draft_body
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await connection.aclose()
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
