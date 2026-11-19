# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the model catalog Flow through a real Dex Worker and Client."""

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

from dex_connectors.dimleap import ListModelsOutput, new_project_connection

from .flow import CONNECTION_NAME, ModelCatalogFlow
from .start import start_model_catalog


@pytest.mark.asyncio
async def test_catalog_retry_and_completion_through_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """A 503 retries the Bearer GET; the Flow stores only current model IDs."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.host == "api.dimleap.cn"
        assert request.url.path == "/v1/models"
        assert request.headers["Authorization"] == "Bearer sk-test-secret"
        assert request.headers["Accept"] == "application/json"
        if len(requests) == 1:
            return httpx.Response(503, headers={"Retry-After": "1"})
        return httpx.Response(200, json={
            "object": "list",
            "data": [{"id": "synthetic-model", "object": "model", "description": "private"}],
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="dimleap-python-dex-") as directory_name:
            directory = Path(directory_name)
            path = directory / "connections.json"
            path.write_text(json.dumps({
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [{
                    "connectorId": "dimleap",
                    "connectionName": CONNECTION_NAME,
                    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/dimleap",
                    "provider": "dimleap",
                    "authMethodId": "dimleap-api-key",
                    "configuration": {},
                    "credentials": {"apiKey": "sk-test-secret"},
                }],
                "triggerBindings": [],
            }))
            path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_client)
            flow = ModelCatalogFlow(connection)
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
                        flow_id = "dimleap-python-" + uuid4().hex
                        assert await start_model_catalog(flow_id)
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        output = result.single_output(ListModelsOutput)
                        assert output.total == 1
                        assert output.model_ids == ("synthetic-model",)
                        assert len(requests) == 2
                        assert "private" not in repr(output)
                        assert "sk-test-secret" not in repr(output)
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
