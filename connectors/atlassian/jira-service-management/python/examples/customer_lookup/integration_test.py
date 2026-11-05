# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the customer lookup against a real Dex Worker and Client."""

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

from dex_connectors.atlassian_jira_service_management import (
    Client,
    Config,
    Credentials,
    CustomerMatches,
    new_connection,
)

from .flow import CONNECTION_NAME, CustomerLookupFlow, CustomerLookupRequest


@pytest.mark.asyncio
async def test_customer_lookup_retries_and_fails_unwired_not_found(tmp_path: Path) -> None:
    """Verify retry, result routing, and optional branch failure across Dex."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[str] = []

    class JiraHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            requests.append(self.path)
            if "missing" in self.path:
                self.send_response(200)
                body = json.dumps({"values": [], "isLastPage": True}).encode()
            elif len(requests) == 1:
                self.send_response(429)
                body = b"{}"
            else:
                self.send_response(200)
                body = json.dumps(
                    {
                        "values": [
                            {
                                "accountId": "jane-1",
                                "displayName": "Jane",
                                "emailAddress": "jane@example.com",
                                "active": True,
                            }
                        ],
                        "isLastPage": True,
                    }
                ).encode()
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, format: str, *args: object) -> None:
            pass  # Provider stand-in logs no request paths or credentials.

    provider = ThreadingHTTPServer(("127.0.0.1", 0), JiraHandler)
    provider_thread = Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("atlassian", CONNECTION_NAME)
    credentials = StaticCredentialProvider(
        {
            reference: Credentials(
                "client-id",
                SecretString("client-secret"),
                SecretString("inert-access-token"),
                SecretString("inert-refresh-token"),
            )
        }
    )
    client = Client(
        Config(
            cloud_id="11223344-a1b2-4b33-8c44-def123456789",
            endpoint=f"http://127.0.0.1:{provider.server_port}",
        ),
        credentials,
    )
    connection = new_connection(client, reference)
    flow = CustomerLookupFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as dex_client:
            success_id = f"jsm-python-found-{uuid4().hex}"
            await dex_client.start_flow(flow, success_id, CustomerLookupRequest("10", "jane@example.com"))
            result = await asyncio.wait_for(dex_client.wait_for_flow(success_id, timedelta(seconds=30)), 40)
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            matches = result.single_output(CustomerMatches)
            assert matches.customers[0].account_id == "jane-1"
            assert len(requests) == 2

            missing_id = f"jsm-python-missing-{uuid4().hex}"
            await dex_client.start_flow(flow, missing_id, CustomerLookupRequest("10", "missing@example.com"))
            missing = await asyncio.wait_for(dex_client.wait_for_flow(missing_id, timedelta(seconds=30)), 40)
            assert missing.status == dex.FlowStatus.FAILED
            assert "notFound" in (missing.error_message or "")
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await connection.aclose()
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
