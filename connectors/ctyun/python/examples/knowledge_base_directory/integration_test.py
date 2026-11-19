# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the signed knowledge-base Query through a real Dex Worker and Client."""

import asyncio
import base64
import hashlib
import hmac
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

from dex_connectors.ctyun import ListKnowledgeBasesOutput, new_project_connection

from .flow import CONNECTION_NAME, KnowledgeBaseDirectoryFlow
from .start import start_knowledge_base_directory


def _hmac(key: bytes, data: bytes) -> bytes:
    return hmac.new(key, data, hashlib.sha256).digest()


@pytest.mark.asyncio
async def test_directory_retry_and_completion_through_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """A 503 retries; the completed Flow stores only public resource metadata."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.host == "kqa-global.ctapi.ctyun.cn"
        assert request.url.path == "/openapi/v1/infobases/list"
        assert request.headers["tenantId"] == "tenant-example"
        assert request.headers["ctyun-eop-request-id"]
        date = request.headers["eop-date"]
        assert len(date) == 16 and date.endswith("Z")
        canonical = (
            f"ctyun-eop-request-id:{request.headers['ctyun-eop-request-id']}\n"
            f"eop-date:{date}\n\n{hashlib.sha256(b'').hexdigest()}"
        )
        time_key = _hmac(b"SKEXAMPLE", date.encode())
        access_key_hash = _hmac(time_key, b"AKIDEXAMPLE")
        date_key = _hmac(access_key_hash, date[:8].encode())
        signature = base64.b64encode(_hmac(date_key, canonical.encode())).decode()
        assert request.headers["Eop-Authorization"] == (
            "AKIDEXAMPLE headers=ctyun-eop-request-id;eop-date Signature=" + signature
        )
        if len(requests) == 1:
            return httpx.Response(503, headers={"Retry-After": "1"})
        return httpx.Response(
            200,
            json={
                "statusCode": "200",
                "returnObj": [{"id": 280, "name": "Example knowledge base", "count": 4}],
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_client:
        with tempfile.TemporaryDirectory(prefix="ctyun-python-dex-") as directory_name:
            directory = Path(directory_name)
            path = directory / "connections.json"
            path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [{
                            "connectorId": "ctyun",
                            "connectionName": CONNECTION_NAME,
                            "modulePath": "github.com/superdurable/dex-connectors-library/connectors/ctyun",
                            "provider": "ctyun",
                            "authMethodId": "ctyun-aksk",
                            "configuration": {},
                            "credentials": {
                                "access_key": "AKIDEXAMPLE",
                                "security_key": "SKEXAMPLE",
                                "tenant_id": "tenant-example",
                            },
                        }],
                        "triggerBindings": [],
                    }
                )
            )
            path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_client)
            flow = KnowledgeBaseDirectoryFlow(connection)
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
                        flow_id = "ctyun-python-" + uuid4().hex
                        assert await start_knowledge_base_directory(flow_id)
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        output = result.single_output(ListKnowledgeBasesOutput)
                        assert len(output.knowledge_bases) == 1
                        assert output.knowledge_bases[0].id == 280
                        assert output.knowledge_bases[0].name == "Example knowledge base"
                        assert output.knowledge_bases[0].file_count == 4
                        assert len(requests) == 2
                        assert "SKEXAMPLE" not in repr(output)
                        assert "tenant-example" not in repr(output)
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
