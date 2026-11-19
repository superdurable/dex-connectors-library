# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the application page Query through a real Dex Worker and Client."""

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

from dex_connectors.fanruan import ListAppsPageOutput, new_project_connection

from .flow import CONNECTION_NAME, AppDirectoryFlow
from .start import start_app_directory


@pytest.mark.asyncio
async def test_directory_retry_and_completion_through_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """A provider rate response retries; the completed Flow stores only app metadata."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "POST"
        assert request.url.host == "api.jiandaoyun.com"
        assert request.url.path == "/api/v5/app/list"
        assert request.headers["Authorization"] == "Bearer secret-api-key"
        assert json.loads(request.content) == {"skip": 3, "limit": 2}
        if len(requests) == 1:
            return httpx.Response(400, json={"code": 8303}, headers={"Retry-After": "1"})
        return httpx.Response(200, json={
            "apps": [
                {"app_id": "app-1", "name": "采购系统", "private": "secret-record"},
                {"app_id": "app-2", "name": "库存系统"},
            ]
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="fanruan-python-dex-") as directory_name:
            directory = Path(directory_name)
            path = directory / "connections.json"
            path.write_text(json.dumps({
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [{
                    "connectorId": "fanruan",
                    "connectionName": CONNECTION_NAME,
                    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/fanruan",
                    "provider": "fanruan",
                    "authMethodId": "jiandaoyun-api-key",
                    "configuration": {},
                    "credentials": {"api_key": "secret-api-key"},
                }],
                "triggerBindings": [],
            }))
            path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_client)
            flow = AppDirectoryFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=address)
                    ) as client:
                        flow_id = "fanruan-python-" + uuid4().hex
                        assert await start_app_directory(flow_id, skip=3, limit=2)
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        output = result.single_output(ListAppsPageOutput)
                        assert [app.id for app in output.apps] == ["app-1", "app-2"]
                        assert output.next_skip == 5
                        assert len(requests) == 2
                        assert "secret-record" not in repr(output)
                        assert "secret-api-key" not in repr(output)
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
