# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the private-safe shop Query through a real Dex Worker and Client."""

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

from dex_connectors.easyapi import ShopResult, new_project_connection

from .flow import CONNECTION_NAME, ShopFlow
from .start import start_shop


@pytest.mark.asyncio
async def test_shop_retry_and_private_output_through_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """A 503 retries the query; only selected service fields reach the Flow output."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    tax_number = "91320211MA1WML8X6T"

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.host == "fapiao-api.easyapi.com"
        assert request.url.path == "/shop/query"
        assert dict(request.url.params) == {
            "appKey": "test-key", "appSecret": "private-secret", "taxNumber": tax_number,
        }
        if len(requests) == 1:
            return httpx.Response(503, headers={"Retry-After": "1"})
        return httpx.Response(200, json={
            "code": 1,
            "message": "private-secret",
            "content": {
                "shopId": 1001, "name": "Example Company", "apiState": 1,
                "endTime": "2027-01-01 00:00:00",
                "identifyNumber": tax_number,
                "address": "private address", "phone": "private phone",
                "bankAccount": "private bank", "drawerName": "private staff",
            },
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="easyapi-python-dex-") as directory_name:
            directory = Path(directory_name)
            path = directory / "connections.json"
            path.write_text(json.dumps({
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [{
                    "connectorId": "easyapi",
                    "connectionName": CONNECTION_NAME,
                    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/easyapi",
                    "provider": "easyapi",
                    "authMethodId": "easyapi-invoice-key",
                    "configuration": {},
                    "credentials": {"app_key": "test-key", "app_secret": "private-secret"},
                }],
                "triggerBindings": [],
            }))
            path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_client)
            flow = ShopFlow(connection)
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
                        flow_id = "easyapi-python-" + uuid4().hex
                        assert await start_shop(flow_id, tax_number)
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        output = result.single_output(ShopResult)
                        assert output.shop_id == 1001
                        assert output.name == "Example Company"
                        assert output.api_state == 1
                        assert len(requests) == 2
                        for private_value in (
                            tax_number, "test-key", "private-secret", "private address",
                            "private phone", "private bank", "private staff",
                        ):
                            assert private_value not in repr(output)
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
