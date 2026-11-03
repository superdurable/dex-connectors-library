# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the policy lookup Flow through a real Dex Worker and Client."""

import asyncio
import json
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.airtable import new_project_connection

from .flow import CONNECTION_NAME, PolicyLookupFlow, PolicyLookupOutcome
from .start import start_policy_lookup


@pytest.mark.asyncio
async def test_policy_lookup_retries_and_fails_unwired_rejection_in_real_dex(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """Verify retry, completion, and optional branch failure across the Dex boundary."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        body = json.loads(request.content)
        formula = body["filterByFormula"]
        if "rejected" in formula:
            return httpx.Response(403, json={"error": {"type": "INVALID_PERMISSIONS"}})
        if len(requests) == 1:
            return httpx.Response(503, json={"error": {"type": "SERVICE_UNAVAILABLE"}})
        return httpx.Response(
            200,
            json={
                "records": [
                    {
                        "id": "recPolicyStandard",
                        "createdTime": "2026-09-01T09:00:00Z",
                        "fields": {"Policy Key": "refund-standard", "Approval Limit USD": 250},
                    }
                ]
            },
        )

    connections_path = tmp_path / "connections.json"
    connections_path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "airtable",
                        "connectionName": CONNECTION_NAME,
                        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/airtable",
                        "provider": "airtable",
                        "authMethodId": "api-key-only",
                        "configuration": {"endpoint": "http://127.0.0.1:22080"},
                        "credentials": {"personal_access_token": "patTEST.token"},
                    }
                ],
                "triggerBindings": [],
            }
        )
    )
    connections_path.chmod(0o600)
    monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(connections_path))

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        project = await load_from_environment()
        connection = new_project_connection(project, CONNECTION_NAME, provider_http_client)
        flow = PolicyLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=20)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                successful_id = f"airtable-python-policy-{uuid4().hex}"
                await start_policy_lookup(successful_id, Path(__file__).with_name("sample-input.json"))
                successful = await asyncio.wait_for(
                    client.wait_for_flow(successful_id, timedelta(seconds=20)), 30
                )
                assert successful.status == dex.FlowStatus.COMPLETED, successful.error_message
                outcome = successful.single_output(PolicyLookupOutcome)
                assert outcome.policy is not None
                assert outcome.policy.id == "recPolicyStandard"
                assert not outcome.is_ambiguous
                assert len(requests) == 2
                assert all(request.method == "POST" for request in requests)

                rejected_id = f"airtable-python-policy-{uuid4().hex}"
                rejected_input = tmp_path / "rejected-input.json"
                rejected_input.write_text(
                    json.dumps(
                        {
                            "baseId": "appRefundBase0001",
                            "tableIdOrName": "tblPolicies000001",
                            "policyKey": "rejected",
                        }
                    )
                )
                await start_policy_lookup(rejected_id, rejected_input)
                rejected = await asyncio.wait_for(
                    client.wait_for_flow(rejected_id, timedelta(seconds=20)), 30
                )
                assert rejected.status == dex.FlowStatus.FAILED
                assert rejected.error_message is not None
                assert "providerRejected" in rejected.error_message
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
            await project.close()
