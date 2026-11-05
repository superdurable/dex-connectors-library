# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise HubSpot routing through a real Dex Worker and Client."""

import asyncio
import json
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.hubspot import CRMObject, GetObjectInput, new_project_connection

from .flow import CONNECTION_NAME, ReadContactFlow


@pytest.mark.asyncio
async def test_read_contact_retries_and_unwired_rejection_fails(monkeypatch: pytest.MonkeyPatch) -> None:
    """Verify a provider retry and an optional branch across the Dex boundary."""
    requests: list[str] = []

    async def provider(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        try:
            header = await reader.readuntil(b"\r\n\r\n")
            request_line = header.split(b"\r\n", 1)[0].decode("ascii")
            requests.append(request_line)
            if "/999?" in request_line:
                status, body = "403 Forbidden", b"{}"
            elif len(requests) == 1:
                status, body = "429 Too Many Requests", b"{}"
            else:
                status = "200 OK"
                body = b'{"id":"501","properties":{"email":"ada@example.com"},"archived":false}'
            response_headers = (
                f"HTTP/1.1 {status}\r\nContent-Type: application/json\r\n"
                f"Content-Length: {len(body)}\r\nConnection: close\r\n\r\n"
            )
            writer.write(response_headers.encode() + body)
            await writer.drain()
        finally:
            writer.close()
            await writer.wait_closed()

    for provider_port in range(44410, 44500):
        try:
            server = await asyncio.start_server(provider, "127.0.0.1", provider_port)
            break
        except OSError:
            continue
    else:
        raise RuntimeError("No provider test port is available in 44410-44499")
    port = server.sockets[0].getsockname()[1]
    try:
        with tempfile.TemporaryDirectory(prefix="hubspot-python-dex-") as directory:
            root = Path(directory)
            connections_path = root / "connections.json"
            connections_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "hubspot",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/hubspot"
                                ),
                                "provider": "hubspot",
                                "authMethodId": "private-app-token",
                                "configuration": {"endpoint": f"http://127.0.0.1:{port}"},
                                "credentials": {"access_token": "pat-na1-inert-test-token"},
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
            flow = ReadContactFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(root / "blobs"), 1 << 24))
            try:
                address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=address)
                    ) as client:
                        flow_id = f"hubspot-python-contact-{uuid4().hex}"
                        await client.start_flow(flow, flow_id, GetObjectInput("contacts", "501", ("email",)))
                        success = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=20)), 30
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        assert success.single_output(CRMObject).id == "501"
                        assert len(requests) == 2
                        rejected_id = f"hubspot-python-contact-{uuid4().hex}"
                        await client.start_flow(flow, rejected_id, GetObjectInput("contacts", "999"))
                        rejected = await asyncio.wait_for(
                            client.wait_for_flow(rejected_id, timedelta(seconds=20)), 30
                        )
                        assert rejected.status == dex.FlowStatus.FAILED
                        assert "providerRejected" in (rejected.error_message or "")
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
    finally:
        server.close()
        await server.wait_closed()
