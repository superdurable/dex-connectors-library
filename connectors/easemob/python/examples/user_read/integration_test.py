# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run token acquisition, refresh, cache, and user read through real Dex."""

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

from dex_connectors.easemob import UserDetail, new_project_connection

from .flow import CONNECTION_NAME, UserReadFlow
from .start import start_user_read


@pytest.mark.asyncio
async def test_user_token_retry_refresh_and_cache_through_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """A 503 retries, a user 401 refreshes, and the next Flow reuses the cached token."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    token_requests: list[httpx.Request] = []
    user_requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        assert request.url.host == "app.example.test"
        if request.url.path == "/demo/chat/token":
            token_requests.append(request)
            assert request.method == "POST"
            assert request.headers["Content-Type"] == "application/json"
            assert json.loads(request.content) == {
                "grant_type": "client_credentials",
                "client_id": "client-id",
                "client_secret": "client-secret",
            }
            if len(token_requests) == 1:
                return httpx.Response(503, headers={"Retry-After": "1"})
            return httpx.Response(200, json={
                "access_token": "token-one" if len(token_requests) == 2 else "token-two",
                "expires_in": 3600,
            })
        assert request.url.path == "/demo/chat/users/user1"
        user_requests.append(request)
        assert request.method == "GET"
        expected_token = "token-one" if len(user_requests) == 1 else "token-two"
        assert request.headers["Authorization"] == "Bearer " + expected_token
        if len(user_requests) == 1:
            return httpx.Response(401)
        return httpx.Response(200, json={
            "count": 1,
            "entities": [{
                "uuid": "uuid-test", "created": 1542795196504,
                "modified": 1542795196504, "username": "user1",
                "activated": True, "nickname": "Sample User",
                "push_token": "PRIVATE-PUSH-TOKEN",
            }],
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="easemob-python-dex-") as directory_name:
            directory = Path(directory_name)
            path = directory / "connections.json"
            path.write_text(json.dumps({
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [{
                    "connectorId": "easemob",
                    "connectionName": CONNECTION_NAME,
                    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/easemob",
                    "provider": "easemob",
                    "authMethodId": "easemob-im-client-credentials",
                    "configuration": {"baseUrl": "https://app.example.test"},
                    "credentials": {
                        "appKey": "demo#chat", "clientId": "client-id",
                        "clientSecret": "client-secret",
                    },
                }],
                "triggerBindings": [],
            }))
            path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_client)
            flow = UserReadFlow(connection)
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
                        for _ in range(2):
                            flow_id = "easemob-python-" + uuid4().hex
                            assert await start_user_read(flow_id, "user1")
                            result = await asyncio.wait_for(
                                client.wait_for_flow(flow_id, timedelta(seconds=40)), 50
                            )
                            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                            output = result.single_output(UserDetail)
                            assert output.user_id == "user1"
                            assert output.user_uuid == "uuid-test"
                            assert output.activated
                            assert "PRIVATE-PUSH-TOKEN" not in repr(output)
                            assert "client-secret" not in repr(output)
                        assert len(token_requests) == 3
                        assert len(user_requests) == 3
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
