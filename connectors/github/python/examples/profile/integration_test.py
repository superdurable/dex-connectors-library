# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the GitHub profile example through a real Dex Worker and Client."""

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

from dex_connectors.github import AuthenticatedProfile, Client, Config, Credentials, new_connection

from .flow import CONNECTION_NAME, ProfileFlow, ProfileRequest


class GitHubStandIn(BaseHTTPRequestHandler):
    """Serve inert profile and email responses on an OS-assigned port."""

    requests: list[str] = []

    def do_GET(self) -> None:
        """Return the two responses needed by the profile operation."""
        self.requests.append(self.path)
        if self.path == "/user":
            body = {"id": 42, "login": "octocat", "name": "Mona"}
            status = 200
        elif self.path == "/user/emails?per_page=100":
            body = [{"email": "octocat@example.com", "primary": True, "verified": True}]
            status = 200
        else:
            body = {}
            status = 404
        payload = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("X-OAuth-Scopes", "read:user, user:email")
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, format: str, *args: object) -> None:
        """Keep inert request details out of test output."""
        return


@pytest.mark.asyncio
async def test_profile_flow_through_real_dex() -> None:
    """Complete the profile Flow using the same application and real Dex stack."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    stand_in = ThreadingHTTPServer(("127.0.0.1", 0), GitHubStandIn)
    server_thread = threading.Thread(target=stand_in.serve_forever, daemon=True)
    server_thread.start()
    reference = ConnectionRef("github", CONNECTION_NAME)
    credentials = Credentials(
        "client-id", SecretString("client-secret"), SecretString("inert-token"), SecretString("")
    )
    client = Client(
        Config(base_url=f"http://127.0.0.1:{stand_in.server_port}"),
        StaticCredentialProvider({reference: credentials}),
    )
    connection = new_connection(client, reference)
    try:
        flow = ProfileFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="github-python-dex-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        flow_id = f"github-python-{uuid4().hex}"
                        await dex_client.start_flow(flow, flow_id, ProfileRequest("test"))
                        result = await asyncio.wait_for(
                            dex_client.wait_for_flow(flow_id, timedelta(seconds=20)), 30
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        profile = result.single_output(AuthenticatedProfile)
                        assert profile.subject == "42" and profile.verified_email == "octocat@example.com"
                        assert GitHubStandIn.requests == ["/user", "/user/emails?per_page=100"]
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
    finally:
        await connection.aclose()
        stand_in.shutdown()
        stand_in.server_close()
        server_thread.join(timeout=5)
