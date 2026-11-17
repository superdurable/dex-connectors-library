# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run a signed status Query through a real Dex Worker and Client."""

import asyncio
import hashlib
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

from dex_connectors.dada import OrderStatus, new_project_connection

from .flow import CONNECTION_NAME, OrderStatusFlow
from .start import start_order_status


@pytest.mark.asyncio
async def test_status_retry_and_completion_through_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """A 503 retries the signed POST, then persists a status without rider data."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    order_id = "orderNo1653708880866"

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "POST"
        assert request.url.host == "newopen.imdada.cn"
        assert request.url.path == "/api/order/status/query"
        assert request.headers["Content-Type"] == "application/json; charset=utf-8"
        envelope = json.loads(request.content)
        assert envelope["app_key"] == "123456"
        assert envelope["source_id"] == "73753"
        assert envelope["format"] == "json"
        assert envelope["v"] == "1.0"
        assert envelope["body"] == json.dumps({"order_id": order_id}, separators=(",", ":"))
        assert "app_secret" not in envelope
        assert envelope["timestamp"].isdecimal()
        fields = (
            "app_key123456body" + envelope["body"] + "formatjsonsource_id73753"
            + "timestamp" + envelope["timestamp"] + "v1.0"
        )
        expected = hashlib.md5(("abcdefg123" + fields + "abcdefg123").encode()).hexdigest().upper()
        assert envelope["signature"] == expected
        if len(requests) == 1:
            return httpx.Response(503, headers={"Retry-After": "1"})
        return httpx.Response(
            200,
            json={
                "status": "success",
                "code": 0,
                "result": {
                    "orderId": order_id,
                    "statusCode": 1,
                    "statusMsg": "待接单",
                    "transporterPhone": "18500000000",
                },
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="dada-python-dex-") as directory_name:
            directory = Path(directory_name)
            path = directory / "connections.json"
            path.write_text(json.dumps({
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [{
                    "connectorId": "dada",
                    "connectionName": CONNECTION_NAME,
                    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/dada",
                    "provider": "dada",
                    "authMethodId": "dada-signed-api",
                    "configuration": {},
                    "credentials": {
                        "appKey": "123456",
                        "appSecret": "abcdefg123",
                        "sourceID": "73753",
                    },
                }],
                "triggerBindings": [],
            }))
            path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_client)
            flow = OrderStatusFlow(connection)
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
                        flow_id = "dada-python-" + uuid4().hex
                        assert await start_order_status(flow_id, order_id)
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        output = result.single_output(OrderStatus)
                        assert output.order_id == order_id
                        assert output.status_code == 1
                        assert output.status_message == "待接单"
                        assert len(requests) == 2
                        assert "18500000000" not in repr(output)
                        assert "abcdefg123" not in repr(output)
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
