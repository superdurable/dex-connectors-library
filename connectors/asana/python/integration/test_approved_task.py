# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise all five Asana operations through a real Dex Worker and Client."""

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

from dex_connectors.asana import Task, new_project_connection
from examples.approved_task.flow import CONNECTION_NAME, ApprovedTaskFlow, ApprovedTaskRequest


@pytest.mark.asyncio
async def test_approved_task_uses_real_dex_and_fails_unwired_optional_branch(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Verify durable transitions, a Query retry, all writes, and optional failure."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    task_id = "1204000000000002"
    project_id = "1201000000000001"
    section_id = "1201000000000101"
    task_name = "[REQ-1042] Replace badge reader"
    task = {
        "gid": task_id,
        "name": task_name,
        "completed": False,
        "created_at": "2026-09-30T16:15:00Z",
        "modified_at": "2026-09-30T16:17:00Z",
        "memberships": [
            {
                "project": {"gid": project_id, "name": "Facilities"},
                "section": {"gid": section_id, "name": "Approved"},
            }
        ],
    }

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        path = request.url.path
        if request.method == "GET" and path.endswith("/tasks"):
            if request.url.params.get("project") == "1201000000000999":
                return httpx.Response(404, json={"errors": []})
            if (
                len([item for item in requests if item.method == "GET" and item.url.path.endswith("/tasks")])
                == 1
            ):
                return httpx.Response(429, headers={"Retry-After": "1"})
            return httpx.Response(200, json={"data": []})
        if request.method == "POST" and path.endswith("/tasks"):
            return httpx.Response(
                201,
                json={
                    "data": {
                        "gid": task_id,
                        "name": task["name"],
                        "permalink_url": f"https://app.asana.com/0/{project_id}/{task_id}",
                        "created_at": task["created_at"],
                    }
                },
            )
        if request.method == "POST" and path.endswith(f"/sections/{section_id}/addTask"):
            return httpx.Response(200, json={"data": {}})
        if request.method == "PUT" and path.endswith(f"/tasks/{task_id}"):
            return httpx.Response(200, json={"data": task})
        if request.method == "POST" and path.endswith(f"/tasks/{task_id}/stories"):
            return httpx.Response(
                201,
                json={
                    "data": {
                        "gid": "1205000000000001",
                        "created_at": "2026-09-30T16:20:00Z",
                        "created_by": {"gid": "1203000000000001", "name": "Ada Lovelace"},
                    }
                },
            )
        if request.method == "GET" and path.endswith(f"/tasks/{task_id}"):
            return httpx.Response(200, json={"data": task})
        return httpx.Response(599)

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        with tempfile.TemporaryDirectory(prefix="asana-python-dex-") as temporary_directory:
            root = Path(temporary_directory)
            connections_path = root / "connections.json"
            connections_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "asana",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/asana"
                                ),
                                "provider": "asana",
                                "configuration": {},
                                "credentials": {"access_token": "asana-inert-token"},
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            connections_path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(connections_path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(connections_path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_http_client)
            flow = ApprovedTaskFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(root / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=20)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=address)
                    ) as client:
                        request = ApprovedTaskRequest(
                            project_id, section_id, task_name, "Approved by facilities."
                        )
                        flow_id = f"asana-python-approved-{uuid4().hex}"
                        await client.start_flow(flow, flow_id, request)
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        final_task = result.single_output(Task)
                        assert final_task.id == task_id
                        assert final_task.memberships[0].section is not None
                        assert final_task.memberships[0].section.id == section_id
                        assert [item.method for item in requests] == [
                            "GET",
                            "GET",
                            "POST",
                            "POST",
                            "PUT",
                            "POST",
                            "GET",
                        ]

                        rejected_id = f"asana-python-rejected-{uuid4().hex}"
                        await client.start_flow(
                            flow,
                            rejected_id,
                            ApprovedTaskRequest(
                                "1201000000000999",
                                section_id,
                                task_name,
                                "Approved by facilities.",
                            ),
                        )
                        rejected = await asyncio.wait_for(
                            client.wait_for_flow(rejected_id, timedelta(seconds=20)),
                            30,
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
