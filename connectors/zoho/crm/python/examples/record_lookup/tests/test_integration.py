# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client coverage for the Zoho CRM example."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.zoho_crm import Client, Config, Credentials, new_connection
from examples.record_lookup.flow import RecordLookupFlow
from examples.record_lookup.models import CONNECTION_NAME, RecordLookupInput, RecordLookupOutput


async def _run_lookup(response: httpx.Response, tmp_path: Path) -> dex.FlowResult:
    reference = ConnectionRef("zoho", CONNECTION_NAME)
    credential = Credentials(
        auth_method_id="zoho-us-oauth", oauth_client_id="inert-client",
        oauth_client_secret=SecretString("inert-secret"), access_token=SecretString("inert-access"),
        refresh_token=SecretString("inert-refresh"), api_domain="https://www.zohoapis.com",
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: response)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credential}), http_client,
                          api_base_url="http://127.0.0.1:12345/crm/v8")
        connection = new_connection(provider, reference)
        flow = RecordLookupFlow(connection)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "cache"), 64 * 1024 * 1024))
        registry = dex.Registry([flow], allow_async_handlers=True)
        address = os.environ.get("DEX_SERVER_ADDRESS", "127.0.0.1:40840")
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        client = dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            flow_id = f"zoho-crm-python-{uuid4().hex}"
            await client.start_flow(flow, flow_id, RecordLookupInput("Contacts", "4150868000000376008"))
            return await client.wait_for_flow(flow_id, timeout=timedelta(seconds=40))
        finally:
            await client.close()
            await worker.close()
            await worker_task
            cache.close()


@pytest.mark.asyncio
async def test_real_dex_record_lookup_completes(tmp_path: Path) -> None:
    """The generated getRecord Step routes its success through a real Worker."""
    result = await _run_lookup(httpx.Response(200, json={"data": [{
        "id": "4150868000000376008", "Email": "jane@acme.example.com",
    }]}), tmp_path)
    assert result.status == dex.FlowStatus.COMPLETED
    assert result.single_output(RecordLookupOutput) == RecordLookupOutput("Contacts", "4150868000000376008")


@pytest.mark.asyncio
async def test_real_dex_unwired_not_found_fails(tmp_path: Path) -> None:
    """An unwired optional notFound branch fails the Flow after a definite 404."""
    result = await _run_lookup(httpx.Response(404, json={"code": "INVALID_DATA", "details": {"api_name": "id"}}), tmp_path)
    assert result.status == dex.FlowStatus.FAILED
    assert result.error_message is not None and "notFound" in result.error_message
