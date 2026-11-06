# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification of the checked-in Excel Flow."""

import asyncio
import json
import os
import threading
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.microsoft_excel import Client, Config, Credentials, new_connection

from .flow import ApprovalDecisionFlow
from .models import CONNECTION_NAME, ApprovalInput, ApprovalOutput


class _ExcelHandler(BaseHTTPRequestHandler):
    requests: list[tuple[str, str]] = []

    def log_message(self, format: str, *args: object) -> None:
        """Suppress HTTP transport details from test output."""

    def do_GET(self) -> None:
        """Serve the two table reads and the summary range read."""
        self._answer()

    def do_PATCH(self) -> None:
        """Accept the absolute summary write."""
        self._answer()

    def do_POST(self) -> None:
        """Accept exactly one decision row append."""
        self._answer()

    def _answer(self) -> None:
        path = self.path.split("?", 1)[0]
        self.requests.append((self.command, path))
        if self.command in ("POST", "PATCH"):
            length = int(self.headers.get("Content-Length", "0"))
            request = json.loads(self.rfile.read(length))
            assert request["values"]
        if path.endswith("/columns"):
            body = {
                "value": [
                    {"id": "col-key", "index": 0, "name": "RequestId"},
                    {"id": "col-amount", "index": 1, "name": "Amount"},
                ]
            }
        elif path.endswith("/dataBodyRange") and "/columns/" in path:
            body = {"address": "Decisions!A2:A1", "rowCount": 0, "columnCount": 1, "values": []}
        elif path.endswith("/dataBodyRange"):
            body = {
                "address": "ApprovalPolicy!A2:B2",
                "rowCount": 1,
                "columnCount": 2,
                "values": [["travel", 500]],
            }
        elif self.command == "GET":
            body = {
                "address": "Summary!A1:B1",
                "rowCount": 1,
                "columnCount": 2,
                "values": [["old", 0]],
                "text": [["old", "0"]],
            }
        else:
            body = {"address": "Summary!A1:B1"}
        payload = json.dumps(body).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


@pytest.mark.asyncio
async def test_real_dex_excel_approval_flow(tmp_path: Path) -> None:
    """Run every Excel operation across a real Dex Worker and Client."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    _ExcelHandler.requests = []
    provider = ThreadingHTTPServer(("127.0.0.1", 0), _ExcelHandler)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("microsoft", CONNECTION_NAME)
    credentials = StaticCredentialProvider({reference: Credentials(access_token=SecretString("inert-token"))})
    try:
        async with httpx.AsyncClient(timeout=20, follow_redirects=False) as http_client:
            connector = Client(
                Config(),
                credentials,
                http_client=http_client,
                local_provider_url=f"http://127.0.0.1:{provider.server_port}",
            )
            flow = ApprovalDecisionFlow(new_connection(connector, reference))
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 25))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            client = dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                flow_id = f"excel-python-approval-{uuid4().hex}"
                await client.start_flow(
                    flow,
                    flow_id,
                    ApprovalInput(
                        "b!driveKey-123_abc",
                        "01BYE5RZ6QN3ZWBTUFOFD3GSPGOHDJD36K",
                        "ApprovalPolicy",
                        "Summary",
                        "A1:B1",
                        "Decisions",
                        "REQ-1",
                        42,
                    ),
                )
                result = await client.wait_for_flow(flow_id, timeout=timedelta(seconds=40))
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(ApprovalOutput) == ApprovalOutput("REQ-1", True, 1)
                assert [method for method, _ in _ExcelHandler.requests] == [
                    "GET",
                    "GET",
                    "GET",
                    "PATCH",
                    "GET",
                    "GET",
                    "POST",
                ]
            finally:
                await client.close()
                await worker.close()
                await worker_task
                cache.close()
    finally:
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
