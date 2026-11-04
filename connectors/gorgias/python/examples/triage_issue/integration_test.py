# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Gorgias ticket triage through a real Dex Worker and Client."""

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

from dex_connectors.gorgias import Client, Config, Credentials, GetTicketInput, TicketDetails, new_connection

from .flow import CONNECTION_NAME, TriageIssueFlow


@pytest.mark.asyncio
async def test_ticket_triage_retries_and_fails_unwired_absence(tmp_path: Path) -> None:
    """Exercise provider retry, persistence, and optional failure routing in Dex."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[str] = []

    class ProviderHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            requests.append(self.path)
            status = 404 if self.path.endswith("/9999") else 503 if len(requests) == 1 else 200
            body = (
                {"error": {"msg": "temporarily_unavailable"}}
                if status == 503
                else {"error": {"msg": "ticket_not_found"}}
                if status == 404
                else {
                    "id": 7001,
                    "status": "open",
                    "priority": "normal",
                    "subject": "Double charge",
                    "customer": {"id": 501, "email": "ada@example.com"},
                    "tags": [],
                    "messages": [],
                    "created_datetime": "2026-01-20T09:00:00",
                }
            )
            encoded = json.dumps(body).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)

        def log_message(self, format: str, *args: object) -> None:
            return

    provider = ThreadingHTTPServer(("127.0.0.1", 0), ProviderHandler)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("gorgias", CONNECTION_NAME)
    provider_client = Client(
        Config("acme"),
        StaticCredentialProvider(
            {reference: Credentials("agent@acme.example.com", SecretString("inert-test-key"))}
        ),
        api_base_url=f"http://127.0.0.1:{provider.server_address[1]}/api",
    )
    connection = new_connection(provider_client, reference)
    flow = TriageIssueFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as client:
            success_id = f"gorgias-python-triage-{uuid4()}"
            await client.start_flow(flow, success_id, GetTicketInput(7001, 5))
            success = await client.wait_for_flow(success_id, timedelta(seconds=30))
            assert success.status == dex.FlowStatus.COMPLETED, success.error_message
            assert success.single_output(TicketDetails).ticket.id == 7001
            assert len(requests) == 2
            absent_id = f"gorgias-python-absent-{uuid4()}"
            await client.start_flow(flow, absent_id, GetTicketInput(9999))
            absent = await client.wait_for_flow(absent_id, timedelta(seconds=30))
            assert absent.status == dex.FlowStatus.FAILED
            assert absent.error_message is not None and "notFound" in absent.error_message
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await connection.aclose()
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
