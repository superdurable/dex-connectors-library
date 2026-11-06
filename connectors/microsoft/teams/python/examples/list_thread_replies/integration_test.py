# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the Teams example through a real Dex Worker and Client."""

import asyncio
import json
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.microsoft_teams import ListThreadRepliesOutput, new_project_connection

from .flow import CONNECTION_NAME, ThreadRepliesFlow, ThreadRequest

TEAM_ID = "fbe2bf47-16c8-47cf-b4a5-4b9b187c508b"
CHANNEL_ID = "19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2"
ROOT_ID = "1616989510408"


@pytest.mark.asyncio
async def test_thread_read_retries_and_unwired_rejection_through_real_dex(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Verify worker registration, retry delivery, persistence, and optional rejection."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    graph_requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        if request.url.host == "login.microsoftonline.com":
            return httpx.Response(
                200,
                json={
                    "access_token": "inert-refreshed-token",
                    "refresh_token": "inert-rotated-token",
                    "token_type": "Bearer",
                    "expires_in": 3600,
                },
            )
        graph_requests.append(request)
        if request.url.path.endswith("/1616989510412/replies"):
            return httpx.Response(404, json={"error": {"code": "NotFound"}})
        if len(graph_requests) == 1:
            return httpx.Response(429, headers={"Retry-After": "1"})
        return httpx.Response(200, json={"value": []})

    with tempfile.TemporaryDirectory(prefix="teams-python-dex-") as directory:
        connections_path = Path(directory) / "connections.json"
        connections_path.write_text(
            json.dumps(
                {
                    "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                    "connections": [
                        {
                            "connectorId": "microsoft-teams",
                            "connectionName": CONNECTION_NAME,
                            "modulePath": (
                                "github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
                            ),
                            "provider": "microsoft",
                            "configuration": {"endpoint": "http://localhost/v1.0"},
                            "credentials": {
                                "oauth_client_id": "inert-client-id",
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
        project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(connections_path)})
        async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
            connection = new_project_connection(project, CONNECTION_NAME, provider_http_client)
            flow = ThreadRepliesFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        flow_id = f"teams-python-list-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow, flow_id, ThreadRequest(TEAM_ID, CHANNEL_ID, ROOT_ID)
                        )
                        success = await asyncio.wait_for(
                            dex_client.wait_for_flow(flow_id, timedelta(seconds=20)), 30
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        assert success.single_output(ListThreadRepliesOutput).replies == ()
                        assert len(graph_requests) == 2

                        rejected_id = f"teams-python-list-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow, rejected_id, ThreadRequest(TEAM_ID, CHANNEL_ID, "1616989510412")
                        )
                        rejected = await asyncio.wait_for(
                            dex_client.wait_for_flow(rejected_id, timedelta(seconds=20)), 30
                        )
                        assert rejected.status == dex.FlowStatus.FAILED
                        assert "notFound" in (rejected.error_message or "")
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
