# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise Trello routing through a real Dex Worker, Client, and retry boundary."""

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

from dex_connectors.atlassian_trello import Card, GetCardInput, new_project_connection

from .flow import CONNECTION_NAME, ApprovedRequestCardFlow

CARD_ID = "6512f0a1c2d3e4f5a6b7c8d9"
BOARD_ID = "6512f0a1c2d3e4f5a6b70001"
LIST_ID = "6512f0a1c2d3e4f5a6b70101"


@pytest.mark.asyncio
async def test_get_card_retries_and_unwired_not_found_fails(monkeypatch: pytest.MonkeyPatch) -> None:
    """Verify a real Dex retry completes and an optional branch fails the Flow."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/000000000000000000000000"):
            return httpx.Response(404, text="not found")
        if len(requests) == 1:
            return httpx.Response(
                429, headers={"Retry-After": "1"}, json={"error": "API_TOKEN_LIMIT_EXCEEDED"}
            )
        return httpx.Response(
            200,
            json={
                "id": CARD_ID,
                "idBoard": BOARD_ID,
                "idList": LIST_ID,
                "name": "Approved request",
                "closed": False,
                "dueComplete": False,
                "pos": 1024,
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="trello-python-dex-") as directory:
            project_path = Path(directory) / "connections.json"
            project_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "trello",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/"
                                    "connectors/atlassian/trello"
                                ),
                                "provider": "trello",
                                "authMethodId": "api-key-token",
                                "configuration": {},
                                "credentials": {"api_key": "test-key", "token": "test-token"},
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            project_path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(project_path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(project_path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_client)
            flow = ApprovedRequestCardFlow(connection)
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
                        successful_id = f"trello-python-card-{uuid4().hex}"
                        await dex_client.start_flow(flow, successful_id, GetCardInput(CARD_ID))
                        success = await asyncio.wait_for(
                            dex_client.wait_for_flow(successful_id, timedelta(seconds=20)), 30
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        assert success.single_output(Card).id == CARD_ID
                        assert len(requests) == 2
                        failed_id = f"trello-python-card-{uuid4().hex}"
                        await dex_client.start_flow(flow, failed_id, GetCardInput("000000000000000000000000"))
                        failed = await asyncio.wait_for(
                            dex_client.wait_for_flow(failed_id, timedelta(seconds=20)), 30
                        )
                        assert failed.status == dex.FlowStatus.FAILED
                        assert "notFound" in (failed.error_message or "")
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
