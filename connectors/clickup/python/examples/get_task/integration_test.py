# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise ClickUp getTask through a real Dex Worker and Client."""

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

from dex_connectors.clickup import Task, new_project_connection

from .flow import CONNECTION_NAME, GetTaskFlow, GetTaskRequest


@pytest.mark.asyncio
async def test_get_task_retries_and_fails_unwired_rejection_in_real_dex(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Cover Worker registration, a rate-limited retry, and an optional branch."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/rejected"):
            return httpx.Response(403, json={"ECODE": "ACCESS_001"})
        if len(requests) == 1:
            return httpx.Response(429, headers={"Retry-After": "1"})
        return httpx.Response(
            200,
            json={
                "id": "86b2x4k7q",
                "name": "Escalation: Fix login",
                "status": {"status": "open", "type": "open"},
                "assignees": [],
                "tags": [],
            },
        )

    with tempfile.TemporaryDirectory(prefix="clickup-python-dex-") as directory:
        connections_path = Path(directory) / "connections.json"
        connections_path.write_text(
            json.dumps(
                {
                    "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                    "connections": [
                        {
                            "connectorId": "clickup",
                            "connectionName": CONNECTION_NAME,
                            "modulePath": "github.com/superdurable/dex-connectors-library/connectors/clickup",
                            "provider": "clickup",
                            "configuration": {},
                            "credentials": {"api_token": "pk_inert_test_token"},
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
            flow = GetTaskFlow(connection)
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
                        successful_flow_id = f"clickup-python-get-{uuid4().hex}"
                        await dex_client.start_flow(flow, successful_flow_id, GetTaskRequest("86b2x4k7q"))
                        success = await asyncio.wait_for(
                            dex_client.wait_for_flow(successful_flow_id, timedelta(seconds=20)), 30
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        assert success.single_output(Task).id == "86b2x4k7q"
                        assert len(requests) == 2

                        rejected_flow_id = f"clickup-python-get-{uuid4().hex}"
                        await dex_client.start_flow(flow, rejected_flow_id, GetTaskRequest("rejected"))
                        rejected = await asyncio.wait_for(
                            dex_client.wait_for_flow(rejected_flow_id, timedelta(seconds=20)), 30
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
