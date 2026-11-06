# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the Linear example through a real Dex Worker and Client."""

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

from dex_connectors.linear import ListWorkflowStatesInput, ListWorkflowStatesOutput, new_project_connection

from .flow import CONNECTION_NAME, ListWorkflowStatesFlow

TEAM_ID = "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"
STATE_ID = "22222222-2222-4222-8222-222222222222"


@pytest.mark.asyncio
async def test_example_lists_states_and_fails_unwired_rejection(monkeypatch: pytest.MonkeyPatch) -> None:
    """Verify real Worker dispatch, retry, persistence, and optional branch failure."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        document = json.loads(request.content)
        if document["variables"]["teamId"] != TEAM_ID:
            return httpx.Response(401, json={"errors": [{"extensions": {"code": "UNAUTHENTICATED"}}]})
        if len(requests) == 1:
            return httpx.Response(429, headers={"Retry-After": "1"}, json={"errors": []})
        return httpx.Response(
            200,
            json={
                "data": {
                    "workflowStates": {
                        "nodes": [
                            {
                                "id": STATE_ID,
                                "name": "Todo",
                                "type": "unstarted",
                                "position": 1,
                                "color": "#eeeeee",
                                "team": {"id": TEAM_ID},
                            },
                        ]
                    }
                }
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        with tempfile.TemporaryDirectory(prefix="linear-python-dex-") as temporary_directory:
            directory = Path(temporary_directory)
            config_path = directory / "connections.json"
            config_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "linear",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/linear"
                                ),
                                "provider": "linear",
                                "authMethodId": "personal-api-key",
                                "configuration": {},
                                "credentials": {
                                    "api_key": "lin_" + "api_" + "SENTINELKEY0123456789abcdefghijklmnopqrst"
                                },
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            config_path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(config_path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(config_path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_http_client)
            flow = ListWorkflowStatesFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as client:
                        success_id = f"linear-python-states-{uuid4().hex}"
                        await client.start_flow(flow, success_id, ListWorkflowStatesInput(TEAM_ID))
                        success = await asyncio.wait_for(
                            client.wait_for_flow(success_id, timedelta(seconds=25)), 30
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        states = success.single_output(ListWorkflowStatesOutput)
                        assert states.team_id == TEAM_ID
                        assert len(states.states) == 1 and states.states[0].id == STATE_ID
                        assert len(requests) == 2

                        failure_id = f"linear-python-states-{uuid4().hex}"
                        await client.start_flow(
                            flow, failure_id, ListWorkflowStatesInput("00000000-0000-4000-8000-000000000000")
                        )
                        failed = await asyncio.wait_for(
                            client.wait_for_flow(failure_id, timedelta(seconds=20)), 30
                        )
                        assert failed.status == dex.FlowStatus.FAILED
                        assert "providerRejected" in (failed.error_message or "")
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
