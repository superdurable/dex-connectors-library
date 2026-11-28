# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Workday Python Flow."""

import asyncio
import base64
import json
import os
from datetime import timedelta
from pathlib import Path
from urllib.parse import parse_qs
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk.projectconfig import load_dex_web_project

from dex_connectors.workday import (
    GetWorkerInput,
    WorkerPage,
    WorkerSummary,
    new_project_connection,
)

from .flow import CONNECTION_NAME, WorkerFlow
from .list_flow import WorkerListFlow, WorkerPageRequest


@pytest.mark.asyncio
async def test_worker_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    token_requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/token":
            token_requests.append(request)
            assert request.method == "POST"
            assert (
                request.headers["Authorization"]
                == "Basic " + base64.b64encode(b"synthetic-client:synthetic-secret").decode()
            )
            assert parse_qs(request.content.decode()) == {
                "grant_type": ["refresh_token"],
                "refresh_token": ["synthetic-refresh"],
            }
            return httpx.Response(
                200,
                json={
                    "access_token": "synthetic-workday-token",
                    "token_type": "Bearer",
                    "expires_in": 3600,
                },
            )
        assert request.headers["Authorization"] == "Bearer synthetic-workday-token"
        assert request.extensions["timeout"]["read"] == 1.732
        if request.url.path == "/api/common/v1/demo/workers":
            requests.append(request)
            assert request.method == "GET"
            assert dict(request.url.params) == {"limit": "2", "offset": "5", "search": " Li+ "}
            if len(requests) == 3:
                return httpx.Response(503)
            return httpx.Response(
                200,
                json={
                    "total": 10,
                    "data": [{"id": "a" * 32, "descriptor": "Li", "privateField": "excluded"}],
                },
            )
        assert request.url.path == "/api/common/v1/demo/workers/0123456789abcdef0123456789abcdef"
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200, json={"id": "0123456789abcdef0123456789abcdef", "descriptor": "Ada Lovelace"}
        )

    base_url = "http://127.0.0.1:12345"
    path = tmp_path / "connections.json"
    with path.open("x") as connections:
        path.chmod(0o600)
        json.dump(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "workday",
                        "connectionName": CONNECTION_NAME,
                        "provider": "workday",
                        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/workday/go",
                        "configuration": {
                            "baseUrl": base_url + "/api/common/v1/demo",
                            "authorizationEndpoint": base_url + "/authorize",
                            "tokenEndpoint": "https://tenant.example.com/token",
                            "scopes": ["Staffing"],
                            "timeout": 1732000000,
                        },
                        "credentials": {
                            "oauth_client_id": "synthetic-client",
                            "oauth_client_secret": "synthetic-secret",
                            "access_token": "expired",
                            "refresh_token": "synthetic-refresh",
                        },
                        "credentialExpiresAt": "2000-01-01T00:00:00Z",
                    }
                ],
            },
            connections,
        )
    project = await load_dex_web_project(path)
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        connection = new_project_connection(project, CONNECTION_NAME, http_client)
        flow = WorkerFlow(connection)
        page_flow = WorkerListFlow(connection)
        registry = dex.Registry([flow, page_flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"workday-python-{uuid4().hex}"
                await client.start_flow(
                    flow, flow_id, GetWorkerInput(worker_id="0123456789abcdef0123456789abcdef")
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(WorkerSummary) == WorkerSummary(
                    "0123456789abcdef0123456789abcdef", "Ada Lovelace"
                )
                assert len(requests) == 2
                page_id = f"workday-python-page-{uuid4().hex}"
                await client.start_flow(
                    page_flow, page_id, WorkerPageRequest(limit=2, offset=5, search=" Li+ ")
                )
                page = await asyncio.wait_for(client.wait_for_flow(page_id, timedelta(seconds=25)), 30)
                assert page.status == dex.FlowStatus.COMPLETED, page.error_message
                assert page.single_output(WorkerPage) == WorkerPage(10, (WorkerSummary("a" * 32, "Li"),))
                assert len(requests) == 4
                assert len(token_requests) == 1
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
            await project.close()
