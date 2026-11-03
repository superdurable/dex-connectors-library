# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the Front example through a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Thread
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.front import Client, Config, Credentials, FindContactByEmailOutput, new_connection

from .flow import CONNECTION_NAME, ContactLookupFlow, ContactLookupRequest


@pytest.mark.asyncio
async def test_contact_lookup_retries_and_fails_unwired_not_found(tmp_path: Path) -> None:
    """Verify durable retry and optional branch routing against a local provider."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[str] = []

    class FrontHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            requests.append(self.path)
            if "missing" in self.path:
                self.send_response(404)
                self.end_headers()
                return
            if len(requests) == 1:
                self.send_response(429)
                self.end_headers()
                return
            body = (
                b'{"id":"crd_100","name":"Jane","handles":'
                b'[{"handle":"jane@acme.example.com","source":"email"}]}'
            )
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, format: str, *args: object) -> None:
            pass  # The stand-in logs no request details or credentials.

    provider = ThreadingHTTPServer(("127.0.0.1", 0), FrontHandler)
    provider_thread = Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("front", CONNECTION_NAME)
    credentials = StaticCredentialProvider({reference: Credentials(SecretString("front-inert-test-token"))})
    client = Client(Config(), credentials, api_base_url=f"http://127.0.0.1:{provider.server_port}")
    connection = new_connection(client, reference)
    flow = ContactLookupFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as dex_client:
            success_id = f"front-python-found-{uuid4().hex}"
            await dex_client.start_flow(flow, success_id, ContactLookupRequest("jane@acme.example.com"))
            success = await asyncio.wait_for(dex_client.wait_for_flow(success_id, timedelta(seconds=30)), 40)
            assert success.status == dex.FlowStatus.COMPLETED, success.error_message
            output = success.single_output(FindContactByEmailOutput)
            assert output.contacts[0].id == "crd_100"
            assert requests.count("/contacts/alt:email:jane@acme.example.com") == 2

            missing_id = f"front-python-missing-{uuid4().hex}"
            await dex_client.start_flow(flow, missing_id, ContactLookupRequest("missing@acme.example.com"))
            missing = await asyncio.wait_for(dex_client.wait_for_flow(missing_id, timedelta(seconds=30)), 40)
            assert missing.status == dex.FlowStatus.FAILED
            assert missing.error_message is not None and "notFound" in missing.error_message
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await connection.aclose()
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
