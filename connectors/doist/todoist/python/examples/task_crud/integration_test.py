# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise all five Todoist operations through a real Dex Worker and Client."""

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

from dex_connectors.doist_todoist import DeleteTaskOutcome, new_project_connection

from .flow import CONNECTION_NAME, TaskCRUDFlow
from .start import start_task_crud


@pytest.mark.asyncio
async def test_task_crud_retries_create_and_deletes_in_real_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """Verify durable routing, stable request identity, and all provider calls."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    created = {
        "id": "task-123",
        "project_id": "project-456",
        "content": "Disposable Todoist task",
        "description": "Created by the Python CRUD example",
        "checked": False,
    }

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        path = request.url.path
        if request.method == "POST" and path.endswith("/tasks"):
            if (
                len([sent for sent in requests if sent.method == "POST" and sent.url.path.endswith("/tasks")])
                == 1
            ):
                return httpx.Response(429, headers={"Retry-After": "1"})
            return httpx.Response(200, json=created)
        if request.method == "GET" and path.endswith("/tasks/task-123"):
            return httpx.Response(200, json=created)
        if request.method == "POST" and path.endswith("/tasks/task-123"):
            updated = {**created, "content": "Disposable Todoist task - updated"}
            return httpx.Response(200, json=updated)
        if request.method == "GET" and path.endswith("/tasks"):
            return httpx.Response(200, json={"results": [created], "next_cursor": ""})
        if request.method == "DELETE" and path.endswith("/tasks/task-123"):
            return httpx.Response(204)
        raise AssertionError(f"unexpected provider request {request.method} {path}")

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        with tempfile.TemporaryDirectory(prefix="todoist-python-dex-") as directory:
            connection_path = Path(directory) / "connections.json"
            connection_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "todoist",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/doist/todoist"
                                ),
                                "provider": "todoist",
                                "authMethodId": "api-key-only",
                                "configuration": {},
                                "credentials": {"api_token": "todoist-inert-test-token"},
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            connection_path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(connection_path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(connection_path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_http_client)
            registry = dex.Registry([TaskCRUDFlow(connection)], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as client:
                        flow_id = f"todoist-python-{uuid4().hex}"
                        input_file = Path(__file__).with_name("sample-input.json")
                        assert await start_task_crud(flow_id, input_file)
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        assert result.single_output(DeleteTaskOutcome).task_id == "task-123"
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
    assert [request.method for request in requests] == ["POST", "POST", "GET", "POST", "GET", "DELETE"]
    assert requests[0].headers["X-Request-ID"] == requests[1].headers["X-Request-ID"]
    assert all("todoist-inert-test-token" not in str(request.url) for request in requests)
