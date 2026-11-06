# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise all five operations through a real Dex Worker and Client."""

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

from dex_connectors.monday import Item, new_project_connection
from examples.work_request.flow import CONNECTION_NAME, WorkRequest, WorkRequestFlow


@pytest.mark.asyncio
async def test_work_request_uses_real_dex_and_fails_an_unwired_branch(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Verify durable transitions, five provider calls, and optional branch failure."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    board_id = "1234567890"
    item_id = "9876543210"
    item = {
        "id": item_id,
        "name": "Review request",
        "state": "active",
        "board": {"id": board_id},
        "column_values": [],
    }

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        document = json.loads(request.content)
        query = document["query"]
        if "ListBoardItems" in query:
            if document["variables"]["boardIds"] == ["9999999999"]:
                return httpx.Response(200, json={"data": {"boards": []}})
            return httpx.Response(
                200,
                json={"data": {"boards": [{"id": board_id, "items_page": {"cursor": None, "items": []}}]}},
            )
        if "CreateItem" in query:
            return httpx.Response(200, json={"data": {"create_item": item}})
        if "UpdateItemColumnValues" in query:
            return httpx.Response(200, json={"data": {"change_multiple_column_values": item}})
        if "AddUpdate" in query:
            return httpx.Response(
                200, json={"data": {"create_update": {"id": "7654321", "item_id": item_id}}}
            )
        if "GetItem" in query:
            return httpx.Response(200, json={"data": {"items": [item]}})
        return httpx.Response(599)

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        with tempfile.TemporaryDirectory(prefix="monday-python-dex-") as temporary_directory:
            root = Path(temporary_directory)
            connections_path = root / "connections.json"
            connections_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "monday",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/monday"
                                ),
                                "provider": "monday",
                                "configuration": {},
                                "credentials": {
                                    "auth_method": "personal-api-token",
                                    "api_token": "monday-inert-token",
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
            connection = new_project_connection(project, CONNECTION_NAME, provider_http_client)
            flow = WorkRequestFlow(connection)
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
                        flow_id = f"monday-python-work-{uuid4().hex}"
                        await client.start_flow(
                            flow, flow_id, WorkRequest(board_id, "Review request", "Ready for review")
                        )
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        assert result.single_output(Item).id == item_id
                        assert len(requests) == 5
                        rejected_id = f"monday-python-rejected-{uuid4().hex}"
                        await client.start_flow(
                            flow, rejected_id, WorkRequest("9999999999", "Review request", "Ready for review")
                        )
                        rejected = await asyncio.wait_for(
                            client.wait_for_flow(rejected_id, timedelta(seconds=20)), 30
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
