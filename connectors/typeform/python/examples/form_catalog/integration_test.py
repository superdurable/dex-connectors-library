# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the form catalog example through real Dex Worker and Client routing."""

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

from dex_connectors.typeform import FormPage, new_project_connection

from .flow import CONNECTION_NAME, FormCatalogFlow, FormCatalogRequest


@pytest.mark.asyncio
async def test_form_catalog_uses_real_dex_and_retries(monkeypatch: pytest.MonkeyPatch) -> None:
    """Verify retry, form output, and Worker registration across the real service."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(429, headers={"Retry-After": "1"})
        return httpx.Response(
            200,
            json={
                "total_items": 1,
                "page_count": 1,
                "items": [{"id": "form123", "title": "Signup", "settings": {"is_public": True}}],
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        with tempfile.TemporaryDirectory(prefix="typeform-python-dex-") as directory:
            connections_path = Path(directory) / "connections.json"
            connections_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "typeform",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/typeform"
                                ),
                                "provider": "typeform",
                                "authMethodId": "personal-access-token",
                                "configuration": {},
                                "credentials": {"access_token": "inert-test-token"},
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
            flow = FormCatalogFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as client:
                        flow_id = f"typeform-python-catalog-{uuid4().hex}"
                        await client.start_flow(flow, flow_id, FormCatalogRequest())
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=20)), 30
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        page = result.single_output(FormPage)
                        assert page.forms[0].id == "form123"
                        assert len(requests) == 2
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
