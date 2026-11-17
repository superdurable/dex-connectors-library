# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the checked-in balance Flow against a real Dex Worker and fake provider."""

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

from dex_connectors.chuanglan import GetBalanceOutput, new_project_connection

from .flow import CONNECTION_NAME, BalanceFlow
from .start import start_balance


@pytest.mark.asyncio
async def test_balance_retry_and_completion_through_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """A 503 retries the read; the Flow records only the remaining count."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "POST"
        assert str(request.url) == "https://sms.example.test/msg/balance/json"
        assert request.headers["Content-Type"] == "application/json"
        assert request.headers["Accept"] == "application/json"
        assert "Authorization" not in request.headers
        assert json.loads(request.content) == {"account": "N9000123", "password": "inert-password"}
        if len(requests) == 1:
            return httpx.Response(503, headers={"Retry-After": "1"})
        return httpx.Response(200, json={"code": 0, "balance": "953", "errorMsg": ""})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="chuanglan-python-dex-") as directory_name:
            directory = Path(directory_name)
            path = directory / "connections.json"
            path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "chuanglan",
                                "connectionName": CONNECTION_NAME,
                            "modulePath": (
                                "github.com/superdurable/dex-connectors-library/"
                                "connectors/chuanglan"
                            ),
                                "provider": "chuanglan",
                                "authMethodId": "chuanglan-sms",
                                "configuration": {"baseUrl": "https://sms.example.test"},
                                "credentials": {"account": "N9000123", "password": "inert-password"},
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_client)
            flow = BalanceFlow(connection)
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
                        flow_id = "chuanglan-python-" + uuid4().hex
                        assert await start_balance(flow_id)
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        output = result.single_output(GetBalanceOutput)
                        assert output.remaining_messages == 953
                        assert len(requests) == 2
                        assert "inert-password" not in repr(output)
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
