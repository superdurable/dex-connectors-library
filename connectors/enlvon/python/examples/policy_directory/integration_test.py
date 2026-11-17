# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run policy search through a real Dex Worker and Client."""

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

from dex_connectors.enlvon import SearchPoliciesOutput, new_project_connection

from .flow import CONNECTION_NAME, PolicyDirectoryFlow
from .start import start_policy_directory


@pytest.mark.asyncio
async def test_policy_quota_retry_and_filtered_completion(monkeypatch: pytest.MonkeyPatch) -> None:
    """A 429 retries and the completed Flow contains only source-linked card metadata."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.host == "enlvon.com"
        assert request.url.path == "/api/search"
        assert request.url.params["q"] == "分时电价"
        assert request.url.params["top_k"] == "1"
        assert request.headers["X-API-Key"] == "enl_test-secret"
        assert "enl_test-secret" not in str(request.url)
        if len(requests) == 1:
            return httpx.Response(429, headers={"Retry-After": "1"})
        return httpx.Response(200, json={
            "count": 1,
            "results": [{
                "标题": "政策 A", "日期": "2026-01-01", "来源": "ndrc",
                "原文链接": "https://www.ndrc.gov.cn/example",
                "核心要点": ["private analysis"],
                "电力实体": {"private": "omitted"},
            }],
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="enlvon-python-dex-") as directory_name:
            directory = Path(directory_name)
            path = directory / "connections.json"
            path.write_text(json.dumps({
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [{
                    "connectorId": "enlvon",
                    "connectionName": CONNECTION_NAME,
                    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/enlvon",
                    "provider": "enlvon",
                    "authMethodId": "enlvon-api",
                    "configuration": {},
                    "credentials": {"api_key": "enl_test-secret"},
                }],
                "triggerBindings": [],
            }))
            path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_client)
            flow = PolicyDirectoryFlow(connection)
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
                        flow_id = "enlvon-python-" + uuid4().hex
                        assert await start_policy_directory(flow_id, "分时电价", 1)
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        output = result.single_output(SearchPoliciesOutput)
                        assert len(output.policies) == 1
                        assert output.policies[0].title == "政策 A"
                        assert output.policies[0].source == "ndrc"
                        assert len(requests) == 2
                        assert "private analysis" not in repr(output)
                        assert "enl_test-secret" not in repr(output)
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
