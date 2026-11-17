# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the checked-in balance Flow against a real Dex Worker and fake provider."""

import asyncio
import base64
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

from dex_connectors.cloopen import GetBalanceOutput, new_project_connection

from .flow import CONNECTION_NAME, BalanceFlow
from .start import start_balance


@pytest.mark.asyncio
async def test_balance_retry_and_completion_through_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """A 503 retries the signed GET; the Flow records only the decimal balance."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.host == "api.example.test"
        assert request.url.path == "/2013-12-26/Accounts/abcdefghijklmnopqrstuvwxyz012345/AccountInfo"
        assert request.headers["Content-Type"] == "application/json;charset=utf-8"
        assert request.headers["Accept"] == "application/json"
        authorization = base64.b64decode(request.headers["Authorization"], validate=True).decode()
        account_sid, timestamp = authorization.split(":")
        assert account_sid == "abcdefghijklmnopqrstuvwxyz012345"
        assert len(timestamp) == 14 and timestamp.isdigit()
        expected_sig = (
            hashlib.md5((account_sid + "012345abcdefghijklmnopqrstuvwxyz" + timestamp).encode())
            .hexdigest()
            .upper()
        )
        assert request.url.params["sig"] == expected_sig
        assert not request.content
        if len(requests) == 1:
            return httpx.Response(503, headers={"Retry-After": "1"})
        return httpx.Response(
            200,
            json={
                "statusCode": "000000",
                "Account": {"balance": "211.00", "subBalance": "112", "friendlyName": "private@example.test"},
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="cloopen-python-dex-") as directory_name:
            directory = Path(directory_name)
            path = directory / "connections.json"
            path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "cloopen",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/cloopen"
                                ),
                                "provider": "cloopen",
                                "authMethodId": "cloopen-account",
                                "configuration": {"baseUrl": "https://api.example.test"},
                                "credentials": {
                                    "account_sid": "abcdefghijklmnopqrstuvwxyz012345",
                                    "auth_token": "012345abcdefghijklmnopqrstuvwxyz",
                                },
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
                        flow_id = "cloopen-python-" + uuid4().hex
                        assert await start_balance(flow_id)
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        output = result.single_output(GetBalanceOutput)
                        assert output.balance == "211.00"
                        assert len(requests) == 2
                        assert "private@example.test" not in repr(output)
                        assert "012345abcdefghijklmnopqrstuvwxyz" not in repr(output)
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
