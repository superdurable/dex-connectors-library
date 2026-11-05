# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the example through a real Dex Worker, Client, retry, and branch route."""

import asyncio
import json
import os
import tempfile
import threading
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.docusign import Envelope, GetEnvelopeInput, new_project_connection

from .flow import CONNECTION_NAME, EnvelopeStatusFlow

ACCOUNT_ID = "a4ec37d6-1111-2222-3333-143885c220e1"
ENVELOPE_ID = "93be49ab-1111-2222-3333-f752070d71ec"


class _ProviderStandIn(BaseHTTPRequestHandler):
    requests: list[str] = []

    def do_GET(self) -> None:
        """Serve one account, a rate limited first read, and a later envelope."""
        self.requests.append(self.path)
        body: dict[str, object]
        if urlsplit(self.path).path == "/oauth/userinfo":
            status, body = (
                200,
                {
                    "accounts": [
                        {"account_id": ACCOUNT_ID, "is_default": True, "base_uri": "https://na3.docusign.net"}
                    ]
                },
            )
        elif "000000000404" in self.path:
            status, body = 404, {"errorCode": "ENVELOPE_DOES_NOT_EXIST"}
        elif sum("/envelopes/" in path and ENVELOPE_ID in path for path in self.requests) == 1:
            status, body = 429, {"errorCode": "RATE_LIMITED"}
        else:
            status, body = (
                200,
                {
                    "envelopeId": ENVELOPE_ID,
                    "status": "completed",
                    "completedDateTime": "2026-10-04T12:00:00Z",
                },
            )
        encoded = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def do_POST(self) -> None:
        """Refresh an inert OAuth token using the manifest's generated driver."""
        self.requests.append(self.path)
        length = int(self.headers.get("Content-Length", "0"))
        self.rfile.read(length)
        encoded = json.dumps(
            {
                "access_token": "inert-refreshed-access-token",
                "refresh_token": "inert-refreshed-refresh-token",
                "token_type": "Bearer",
                "scope": "signature extended",
                "expires_in": 3600,
            }
        ).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def log_message(self, _format: str, *args: object) -> None:
        """Keep the test log free of request headers."""


class _LocalProviderTransport(httpx.AsyncBaseTransport):
    def __init__(self, port: int) -> None:
        self.port = port
        self.transport = httpx.AsyncHTTPTransport()

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        """Preserve the provider path while sending bytes to the local stand-in."""
        local_url = f"http://127.0.0.1:{self.port}{request.url.raw_path.decode()}"
        headers = [(name, value) for name, value in request.headers.raw if name.lower() != b"host"]
        local_request = httpx.Request(
            request.method, local_url, headers=headers, content=await request.aread()
        )
        return await self.transport.handle_async_request(local_request)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        await self.transport.aclose()


@pytest.mark.asyncio
async def test_envelope_status_retries_and_fails_unwired_not_found(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Route the happy path and optional failure through Dex persistence."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    _ProviderStandIn.requests = []
    provider = ThreadingHTTPServer(("127.0.0.1", 0), _ProviderStandIn)
    server_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    server_thread.start()
    try:
        async with httpx.AsyncClient(
            transport=_LocalProviderTransport(provider.server_port)
        ) as provider_http:
            with tempfile.TemporaryDirectory(prefix="docusign-python-dex-") as directory:
                connections_path = Path(directory) / "connections.json"
                connections_path.write_text(
                    json.dumps(
                        {
                            "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                            "connections": [
                                {
                                    "connectorId": "docusign",
                                    "connectionName": CONNECTION_NAME,
                                    "modulePath": (
                                        "github.com/superdurable/dex-connectors-library/"
                                        "connectors/docusign"
                                    ),
                                    "provider": "docusign",
                                    "authMethodId": "docusign-oauth",
                                    "configuration": {},
                                    "credentials": {
                                        "oauth_client_id": "inert-integration-key",
                                        "oauth_client_secret": "inert-client-secret",
                                        "access_token": "inert-access-token",
                                        "refresh_token": "inert-refresh-token",
                                    },
                                }
                            ],
                            "triggerBindings": [],
                        }
                    )
                )
                connections_path.chmod(0o600)
                monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(connections_path))
                project = await load_from_environment()
                connection = new_project_connection(project, CONNECTION_NAME, provider_http)
                flow = EnvelopeStatusFlow(connection)
                registry = dex.Registry([flow], allow_async_handlers=True)
                cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
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
                            flow_id = f"docusign-python-{uuid4().hex}"
                            await dex_client.start_flow(flow, flow_id, GetEnvelopeInput(ENVELOPE_ID))
                            result = await asyncio.wait_for(
                                dex_client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                            )
                            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                            assert result.single_output(Envelope).status == "completed"
                            assert "/oauth/token" in _ProviderStandIn.requests
                            assert sum(ENVELOPE_ID in path for path in _ProviderStandIn.requests) == 2

                            missing_id = "93be49ab-1111-2222-3333-000000000404"
                            failed_id = f"docusign-python-{uuid4().hex}"
                            await dex_client.start_flow(flow, failed_id, GetEnvelopeInput(missing_id))
                            failed = await asyncio.wait_for(
                                dex_client.wait_for_flow(failed_id, timedelta(seconds=30)), 40
                            )
                            assert failed.status == dex.FlowStatus.FAILED
                            assert failed.error_message is not None
                            assert "notFound" in failed.error_message
                    finally:
                        await worker.stop()
                        await worker_task
                finally:
                    cache.close()
                await connection.aclose()
                await project.close()
    finally:
        provider.shutdown()
        provider.server_close()
        server_thread.join(timeout=5)
