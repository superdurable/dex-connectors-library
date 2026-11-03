# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the BambooHR example through a real Dex Worker and Client."""

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

from dex_connectors.bamboohr import EmployeeRecord, GetEmployeeInput, new_project_connection

from .flow import CONNECTION_NAME, GetEmployeeFlow


@pytest.mark.asyncio
async def test_employee_lookup_retries_and_fails_unwired_not_found(monkeypatch: pytest.MonkeyPatch) -> None:
    """Verify real Dex retry, result decoding, and optional branch failure."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/999"):
            return httpx.Response(404)
        if len(requests) == 1:
            return httpx.Response(429, headers={"Retry-After": "1"})
        return httpx.Response(200, json={"id": "123", "firstName": "Ava", "lastName": "Nguyen"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        with tempfile.TemporaryDirectory(prefix="bamboohr-python-dex-") as directory:
            connections_path = Path(directory) / "connections.json"
            connections_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "bamboohr",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/bamboohr"
                                ),
                                "provider": "bamboohr",
                                "authMethodId": "api-key-only",
                                "configuration": {"companyDomain": "acme"},
                                "credentials": {"api_key": "inert-example-key"},
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
            flow = GetEmployeeFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=address)
                    ) as client:
                        found_id = f"bamboohr-python-get-{uuid4().hex}"
                        await client.start_flow(
                            flow, found_id, GetEmployeeInput("123", ("firstName", "lastName", "ssn"))
                        )
                        found = await asyncio.wait_for(
                            client.wait_for_flow(found_id, timedelta(seconds=20)), 30
                        )
                        assert found.status == dex.FlowStatus.COMPLETED, found.error_message
                        employee = found.single_output(EmployeeRecord)
                        assert employee.employee_id == "123"
                        assert employee.fields == {"firstName": "Ava", "lastName": "Nguyen"}
                        assert employee.omitted_fields == ("ssn",)
                        assert len(requests) == 2

                        missing_id = f"bamboohr-python-get-{uuid4().hex}"
                        await client.start_flow(flow, missing_id, GetEmployeeInput("999", ("firstName",)))
                        missing = await asyncio.wait_for(
                            client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                        )
                        assert missing.status == dex.FlowStatus.FAILED
                        assert missing.error_message is not None and "notFound" in missing.error_message
                        assert len(requests) == 3
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
