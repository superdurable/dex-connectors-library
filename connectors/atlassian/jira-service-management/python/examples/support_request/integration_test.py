# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise Jira Query and Mutation paths through a real Dex Worker and Client."""

import asyncio
import json
import os
import tempfile
import threading
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.atlassian_jira_service_management import (
    Client,
    Config,
    CreateTicketOutput,
    Credentials,
    CustomerMatches,
    new_connection,
)

from .flow import (
    CONNECTION_NAME,
    CreateTicketFlow,
    SupportRequestFlow,
    SupportRequestInput,
    SupportTicketInput,
)

_CLOUD_ID = "11223344-a1b2-4b33-8c44-def123456789"


@pytest.mark.asyncio
async def test_customer_lookup_and_unwired_not_found_in_real_dex() -> None:
    """Verify provider dispatch, successful routing, and optional branch failure."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[str] = []
    create_requests: list[dict[str, object]] = []

    class ProviderHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            requests.append(self.path)
            query = parse_qs(urlsplit(self.path).query)
            values = []
            if query.get("query") == ["jane@acme.example.com"]:
                values.append(
                    {
                        "accountId": "5b10ac8d82e05b22cc7d4ef5",
                        "displayName": "Jane Doe",
                        "emailAddress": "jane@acme.example.com",
                        "active": True,
                    }
                )
            body = json.dumps({"values": values, "isLastPage": True}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self) -> None:
            body_length = int(self.headers["Content-Length"])
            create_requests.append(json.loads(self.rfile.read(body_length)))
            if len(create_requests) == 1:
                status = 429
                response = b"{}"
            else:
                status = 201
                response = b'{"issueId":"10042","issueKey":"ITH-42"}'
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(response)))
            self.end_headers()
            self.wfile.write(response)

        def log_message(self, format: str, *args: object) -> None:
            pass

    provider = ThreadingHTTPServer(("127.0.0.1", 0), ProviderHandler)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("atlassian", CONNECTION_NAME)
    credentials = Credentials(
        "client-id",
        SecretString("inert-secret"),
        SecretString("inert-access-token"),
        SecretString("inert-refresh-token"),
    )
    try:
        async with httpx.AsyncClient(follow_redirects=False) as http_client:
            client = Client(
                Config(cloud_id=_CLOUD_ID, endpoint=f"http://127.0.0.1:{provider.server_port}"),
                StaticCredentialProvider({reference: credentials}),
                http_client,
            )
            connection = new_connection(client, reference)
            flow = SupportRequestFlow(connection)
            create_flow = CreateTicketFlow(connection)
            registry = dex.Registry([flow, create_flow], allow_async_handlers=True)
            with tempfile.TemporaryDirectory(prefix="jsm-python-dex-blobs-") as cache_directory:
                cache = dex.open_blob_cache(
                    dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24)
                )
                try:
                    worker = dex.AsyncWorker(
                        registry, cache, dex.WorkerOptions(server_address=server_address)
                    )
                    worker_task = asyncio.create_task(worker.start())
                    try:
                        await worker.wait_pull_registered(timeout=15)
                        async with dex.AsyncClient(
                            registry, cache, dex.ClientOptions(server_address=server_address)
                        ) as dex_client:
                            found_id = f"jsm-python-{uuid4().hex}"
                            await dex_client.start_flow(
                                flow, found_id, SupportRequestInput("10", "jane@acme.example.com")
                            )
                            found = await asyncio.wait_for(
                                dex_client.wait_for_flow(found_id, timedelta(seconds=20)), 30
                            )
                            assert found.status == dex.FlowStatus.COMPLETED, found.error_message
                            matches = found.single_output(CustomerMatches)
                            assert len(matches.customers) == 1
                            assert matches.customers[0].account_id == "5b10ac8d82e05b22cc7d4ef5"
                            missing_id = f"jsm-python-{uuid4().hex}"
                            await dex_client.start_flow(
                                flow, missing_id, SupportRequestInput("10", "nobody@acme.example.com")
                            )
                            missing = await asyncio.wait_for(
                                dex_client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                            )
                            assert missing.status == dex.FlowStatus.FAILED
                            assert "notFound" in (missing.error_message or "")
                            assert len(requests) == 2
                            created_id = f"jsm-python-{uuid4().hex}"
                            await dex_client.start_flow(
                                create_flow,
                                created_id,
                                SupportTicketInput(
                                    "10", "25", "Laptop will not boot", "My laptop will not boot."
                                ),
                            )
                            created = await asyncio.wait_for(
                                dex_client.wait_for_flow(created_id, timedelta(seconds=25)), 35
                            )
                            assert created.status == dex.FlowStatus.COMPLETED, created.error_message
                            ticket = created.single_output(CreateTicketOutput)
                            assert ticket.issue_id == "10042" and ticket.issue_key == "ITH-42"
                            assert len(create_requests) == 2
                            assert create_requests[0] == create_requests[1]
                    finally:
                        await worker.stop()
                        await worker_task
                finally:
                    cache.close()
    finally:
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
