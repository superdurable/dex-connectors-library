# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the contact lookup Flow through a real Dex Worker and Client."""

import asyncio
import json
import os
import tempfile
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Thread
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.reamaze import (
    Client,
    Config,
    Credentials,
    FindContactByEmailInput,
    FindContactByEmailOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, FindContactFlow


@pytest.mark.asyncio
async def test_contact_lookup_retries_and_routes_optional_rejection() -> None:
    """Check real Dex retry, provider request count, and unwired branch failure."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[str] = []

    class ProviderHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            requests.append(self.path)
            body: dict[str, object]
            if "rejected%40" in self.path:
                status, body = 401, {"error": "unauthorized"}
            elif len(requests) == 1:
                status, body = 429, {"error": "rate limit"}
            else:
                status, body = (
                    200,
                    {
                        "page_size": 30,
                        "page_count": 1,
                        "total_count": 1,
                        "contacts": [{"email": "jane@acme.example.com", "name": "Jane Smith"}],
                    },
                )
            encoded = json.dumps(body).encode()
            self.send_response(status)
            if status == 429:
                self.send_header("Retry-After", "1")
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)

        def log_message(self, format: str, *args: object) -> None:
            """Keep local provider requests out of test logs."""

    provider = ThreadingHTTPServer(("127.0.0.1", 0), ProviderHandler)
    provider_thread = Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("reamaze", CONNECTION_NAME)
    client = Client(
        Config("acme"),
        StaticCredentialProvider(
            {reference: Credentials("agent@acme.example.com", SecretString("inert-token"))}
        ),
        api_base_url=f"http://127.0.0.1:{provider.server_address[1]}/api/v1",
    )
    connection = new_connection(client, reference)
    try:
        flow = FindContactFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="reamaze-python-dex-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=address)
                    ) as dex_client:
                        successful_flow_id = f"reamaze-python-contact-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow, successful_flow_id, FindContactByEmailInput("jane@acme.example.com")
                        )
                        success = await asyncio.wait_for(
                            dex_client.wait_for_flow(successful_flow_id, timedelta(seconds=20)), 30
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        output = success.single_output(FindContactByEmailOutput)
                        assert output.contacts[0].email == "jane@acme.example.com"
                        assert len(requests) == 2

                        rejected_flow_id = f"reamaze-python-contact-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow, rejected_flow_id, FindContactByEmailInput("rejected@acme.example.com")
                        )
                        rejected = await asyncio.wait_for(
                            dex_client.wait_for_flow(rejected_flow_id, timedelta(seconds=20)), 30
                        )
                        assert rejected.status == dex.FlowStatus.FAILED
                        assert "providerRejected" in (rejected.error_message or "")
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
