# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the Pipedrive deal lookup through a real Dex Worker and Client."""

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
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.pipedrive import CRMObject, GetObjectInput, new_project_connection

from .flow import CONNECTION_NAME, DealLookupFlow


@pytest.mark.asyncio
async def test_deal_lookup_retries_and_completes_in_real_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """Prove provider retry, successful routing, and an unwired optional branch."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[str] = []

    class ProviderHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            requests.append(self.path)
            if self.path.endswith("/404"):
                self.send_response(404)
                content = b'{"success":false}'
            elif len(requests) == 1:
                self.send_response(503)
                content = b'{"success":false}'
            else:
                self.send_response(200)
                content = b'{"success":true,"data":{"id":42,"title":"Acme renewal"}}'
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(content)))
            self.end_headers()
            self.wfile.write(content)

        def log_message(self, format: str, *arguments: object) -> None:
            pass  # The stand-in has no useful diagnostics and must not print credentials.

    provider = ThreadingHTTPServer(("127.0.0.1", 0), ProviderHandler)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    try:
        with tempfile.TemporaryDirectory(prefix="pipedrive-python-dex-") as directory:
            root = Path(directory)
            connections_path = root / "connections.json"
            connections_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "pipedrive",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/pipedrive"
                                ),
                                "provider": "pipedrive",
                                "authMethodId": "api-token",
                                "configuration": {"endpoint": f"http://127.0.0.1:{provider.server_port}"},
                                "credentials": {"api_token": "pipedrive-inert-example-token"},
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            connections_path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(connections_path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(connections_path)})
            connection = new_project_connection(project, CONNECTION_NAME)
            flow = DealLookupFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(root / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as client:
                        successful_id = f"pipedrive-python-deal-{uuid4().hex}"
                        await client.start_flow(flow, successful_id, GetObjectInput("deals", "42"))
                        success = await asyncio.wait_for(
                            client.wait_for_flow(successful_id, timedelta(seconds=30)), 40
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        assert success.single_output(CRMObject).id == "42"
                        assert requests == ["/api/v2/deals/42", "/api/v2/deals/42"]

                        missing_id = f"pipedrive-python-deal-{uuid4().hex}"
                        await client.start_flow(flow, missing_id, GetObjectInput("deals", "404"))
                        missing = await asyncio.wait_for(
                            client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                        )
                        assert missing.status == dex.FlowStatus.FAILED
                        assert "notFound" in (missing.error_message or "")
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
        provider_thread.join(timeout=5)
