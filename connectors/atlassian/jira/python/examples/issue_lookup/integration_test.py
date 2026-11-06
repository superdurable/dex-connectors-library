# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Jira lookup Flow against a real Dex Worker and local provider."""

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

from dex_connectors.atlassian_jira import Client, Config, Credentials, GetIssueInput, Issue, new_connection

from .flow import CONNECTION_NAME, IssueLookupFlow

CLOUD_ID = "11223344-a1b2-4b33-8c44-def123456789"


class JiraStandIn(BaseHTTPRequestHandler):
    """Serve one credential-safe issue response on an OS-assigned port."""

    request_count = 0

    def do_GET(self) -> None:
        """Return the issue or a definite missing issue result."""
        type(self).request_count += 1
        if self.path.endswith("/issue/OPS-43") or "/issue/OPS-43?" in self.path:
            body = {
                "id": "10043",
                "key": "OPS-43",
                "fields": {
                    "summary": "Fire panel wiring",
                    "status": {"id": "1", "name": "To Do", "statusCategory": {"key": "new"}},
                    "issuetype": {"id": "10001", "name": "Task"},
                    "project": {"id": "10000", "key": "OPS", "name": "Operations"},
                    "labels": ["incident"],
                    "created": "2026-09-30T09:15:00Z",
                    "updated": "2026-09-30T10:00:00Z",
                },
            }
            payload = json.dumps(body).encode()
            self.send_response(200)
        else:
            payload = b"{}"
            self.send_response(404)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, format: str, *args: object) -> None:
        """Keep provider request details out of test logs."""
        return


@pytest.mark.asyncio
async def test_issue_lookup_through_real_dex() -> None:
    """Verify successful Query delivery and unwired notFound failure."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    provider = ThreadingHTTPServer(("127.0.0.1", 0), JiraStandIn)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    reference = ConnectionRef("atlassian", CONNECTION_NAME)
    credentials = Credentials(
        "client-id",
        SecretString("client-secret"),
        SecretString("jira-access-token"),
        SecretString("refresh-token"),
    )
    client = Client(
        Config(cloud_id=CLOUD_ID, endpoint=f"http://127.0.0.1:{provider.server_port}"),
        StaticCredentialProvider({reference: credentials}),
    )
    connection = new_connection(client, reference)
    flow = IssueLookupFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    try:
        with tempfile.TemporaryDirectory(prefix="jira-python-dex-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        found_id = f"jira-python-found-{uuid4().hex}"
                        await dex_client.start_flow(flow, found_id, GetIssueInput("OPS-43"))
                        found = await asyncio.wait_for(
                            dex_client.wait_for_flow(found_id, timedelta(seconds=20)), 30
                        )
                        assert found.status == dex.FlowStatus.COMPLETED, found.error_message
                        assert found.single_output(Issue).key == "OPS-43"
                        missing_id = f"jira-python-missing-{uuid4().hex}"
                        await dex_client.start_flow(flow, missing_id, GetIssueInput("OPS-404"))
                        missing = await asyncio.wait_for(
                            dex_client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                        )
                        assert missing.status == dex.FlowStatus.FAILED
                        assert missing.error_message and "notFound" in missing.error_message
                        assert JiraStandIn.request_count == 2
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
