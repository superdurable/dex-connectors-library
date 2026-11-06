# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the Xero contact lookup through a real Dex Worker and Client."""

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

from dex_connectors.xero import (
    Client,
    Config,
    Credentials,
    FindContactByEmailInput,
    FindContactByEmailOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ContactLookupFlow


class _XeroHandler(BaseHTTPRequestHandler):
    requests: list[str] = []

    def do_GET(self) -> None:
        """Answer the one contact lookup without logging credentials."""
        self.requests.append(self.path)
        contacts = (
            []
            if "missing" in self.path
            else [
                {
                    "ContactID": "025867f1-d741-4d6b-b1af-9ac774b59ba7",
                    "ContactStatus": "ACTIVE",
                    "Name": "City Agency",
                    "EmailAddress": "accounts@example.com",
                }
            ]
        )
        encoded = json.dumps({"Contacts": contacts}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def log_message(self, _format: str, *_args: object) -> None:
        """Keep test transport details and credentials out of logs."""


@pytest.mark.asyncio
async def test_contact_lookup_runs_in_real_dex() -> None:
    """Verify the happy branch and unwired notFound branch across Worker persistence."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    provider = ThreadingHTTPServer(("127.0.0.1", 0), _XeroHandler)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("xero", CONNECTION_NAME)
    credentials = Credentials(
        auth_method_id="custom-connection", access_token=SecretString("inert-xero-access-token")
    )
    client = Client(
        Config(),
        StaticCredentialProvider({reference: credentials}),
        local_provider_url=f"http://127.0.0.1:{provider.server_address[1]}",
    )
    connection = new_connection(client, reference)
    try:
        with tempfile.TemporaryDirectory(prefix="xero-python-dex-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            try:
                flow = ContactLookupFlow(connection)
                registry = dex.Registry([flow], allow_async_handlers=True)
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        found_id = f"xero-python-found-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow, found_id, FindContactByEmailInput("accounts@example.com")
                        )
                        found = await asyncio.wait_for(
                            dex_client.wait_for_flow(found_id, timedelta(seconds=20)), 30
                        )
                        assert found.status == dex.FlowStatus.COMPLETED, found.error_message
                        output = found.single_output(FindContactByEmailOutput)
                        assert output.contact is not None
                        assert output.contact.contact_id == "025867f1-d741-4d6b-b1af-9ac774b59ba7"
                        missing_id = f"xero-python-missing-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow, missing_id, FindContactByEmailInput("missing@example.com")
                        )
                        missing = await asyncio.wait_for(
                            dex_client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                        )
                        assert missing.status == dex.FlowStatus.FAILED
                        assert "notFound" in (missing.error_message or "")
                        assert len(_XeroHandler.requests) == 2
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
