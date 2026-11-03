# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the Hiver example through a real Dex Worker and Client."""

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

from dex_connectors.hiver import Client, Config, Credentials, ListInboxesOutput, new_connection

from .flow import CONNECTION_NAME, InboxPageFlow, InboxPageRequest


@pytest.mark.asyncio
async def test_inbox_page_retries_and_fails_unwired_rejection(tmp_path: Path) -> None:
    """Verify a retry and optional branch across a real Dex persistence boundary."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[str] = []

    class HiverHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            requests.append(self.path)
            if "next_page=missing" in self.path:
                self.send_response(401)
                self.end_headers()
                return
            if len(requests) == 1:
                self.send_response(429)
                self.end_headers()
                return
            body = (
                b'{"data":{"results":[{"id":101,"display_name":"Support",'
                b'"email":"support@example.com","is_authorised":true}],'
                b'"pagination":{"next_page":null}}}'
            )
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, format: str, *args: object) -> None:
            pass  # The stand-in never logs credentials or request details.

    provider = ThreadingHTTPServer(("127.0.0.1", 0), HiverHandler)
    provider_thread = Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("hiver", CONNECTION_NAME)
    credentials = StaticCredentialProvider({reference: Credentials(SecretString("inertHiverTestKey012345"))})
    client = Client(
        Config(request_interval_milliseconds=1),
        credentials,
        api_base_url=f"http://127.0.0.1:{provider.server_port}/v1",
    )
    connection = new_connection(client, reference)
    flow = InboxPageFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as dex_client:
            success_id = f"hiver-python-listed-{uuid4().hex}"
            await dex_client.start_flow(flow, success_id, InboxPageRequest())
            success = await asyncio.wait_for(dex_client.wait_for_flow(success_id, timedelta(seconds=30)), 40)
            assert success.status == dex.FlowStatus.COMPLETED, success.error_message
            output = success.single_output(ListInboxesOutput)
            assert output.inboxes[0].id == "101"
            assert requests.count("/v1/inboxes?limit=50") == 2

            rejected_id = f"hiver-python-rejected-{uuid4().hex}"
            await dex_client.start_flow(flow, rejected_id, InboxPageRequest("missing"))
            rejected = await asyncio.wait_for(
                dex_client.wait_for_flow(rejected_id, timedelta(seconds=30)), 40
            )
            assert rejected.status == dex.FlowStatus.FAILED
            assert rejected.error_message is not None and "providerRejected" in rejected.error_message
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await connection.aclose()
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
