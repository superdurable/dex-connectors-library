# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify QuickBooks Query routing through a real Dex Worker and Client."""

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

from dex_connectors.intuit_quickbooks import Client, Config, Credentials, Invoice, new_connection

from .flow import CONNECTION_NAME, InvoiceLookupFlow, InvoiceLookupInput


class QuickBooksStandIn(BaseHTTPRequestHandler):
    """Return one invoice and one definite not-found answer on a free port."""

    requests: list[str] = []

    def do_GET(self) -> None:
        """Reply with an invoice only for entity 130."""
        self.requests.append(self.path)
        if "/invoice/130?" in self.path:
            status = 200
            body = {"Invoice": {"Id": "130", "CustomerRef": {"value": "58"}, "TotalAmt": 112.4, "Balance": 0}}
        else:
            status = 404
            body = {}
        encoded = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def log_message(self, format: str, *args: object) -> None:
        """Keep test provider logs free of request details."""


@pytest.mark.asyncio
async def test_get_invoice_and_unwired_not_found_in_real_dex() -> None:
    """Exercise provider I/O, success routing, and optional branch failure."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    QuickBooksStandIn.requests = []
    provider = ThreadingHTTPServer(("127.0.0.1", 0), QuickBooksStandIn)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("quickbooks", CONNECTION_NAME)
    credentials = Credentials(
        "test-client",
        SecretString("test-secret"),
        SecretString("inert-access"),
        SecretString("inert-refresh"),
        SecretString(""),
    )
    client = Client(
        Config(realm_id="9341453050298464"),
        StaticCredentialProvider({reference: credentials}),
        local_provider_url=f"http://127.0.0.1:{provider.server_address[1]}",
    )
    connection = new_connection(client, reference)
    flow = InvoiceLookupFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    try:
        with tempfile.TemporaryDirectory(prefix="quickbooks-python-dex-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        found_id = f"quickbooks-python-{uuid4().hex}"
                        await dex_client.start_flow(flow, found_id, InvoiceLookupInput("130"))
                        found = await asyncio.wait_for(
                            dex_client.wait_for_flow(found_id, timedelta(seconds=20)), 30
                        )
                        assert found.status == dex.FlowStatus.COMPLETED, found.error_message
                        invoice = found.single_output(Invoice)
                        assert invoice.invoice_id == "130" and invoice.balance == "0"
                        missing_id = f"quickbooks-python-{uuid4().hex}"
                        await dex_client.start_flow(flow, missing_id, InvoiceLookupInput("999"))
                        missing = await asyncio.wait_for(
                            dex_client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                        )
                        assert missing.status == dex.FlowStatus.FAILED
                        assert "notFound" in (missing.error_message or "")
                        assert len(QuickBooksStandIn.requests) == 2
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
    finally:
        await client.aclose()
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
