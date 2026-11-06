# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client coverage for Zoho CRM retry and optional branches."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.zoho_crm import Client, Config, Credentials, GetRecordInput, Record, new_connection

from .flow import CONNECTION_NAME, RecordLookupFlow


@pytest.mark.asyncio
async def test_record_lookup_retry_and_unwired_not_found(tmp_path: Path) -> None:
    """Run the checked-in Flow through real Dex persistence and Step retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/999"):
            return httpx.Response(204)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(200, json={"data": [{"id": "123", "Stage": "Qualification"}]})

    reference = ConnectionRef("zoho", CONNECTION_NAME)
    credentials = Credentials(
        auth_method_id="zoho-us-oauth",
        oauth_client_id="synthetic-client",
        oauth_client_secret=SecretString("synthetic-secret"),
        access_token=SecretString("synthetic-access"),
        refresh_token=SecretString("synthetic-refresh"),
        api_domain="https://www.zohoapis.com",
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(),
            StaticCredentialProvider({reference: credentials}),
            http_client,
            api_base_url="http://127.0.0.1:12345/crm/v8",
        )
        connection = new_connection(provider, reference)
        flow = RecordLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                found_id = f"zoho-crm-python-{uuid4().hex}"
                await client.start_flow(flow, found_id, GetRecordInput("Deals", "123", ("Stage",)))
                found = await asyncio.wait_for(client.wait_for_flow(found_id, timedelta(seconds=25)), 30)
                assert found.status == dex.FlowStatus.COMPLETED, found.error_message
                assert found.single_output(Record).id == "123"
                assert len(requests) == 2

                missing_id = f"zoho-crm-python-{uuid4().hex}"
                await client.start_flow(flow, missing_id, GetRecordInput("Deals", "999"))
                missing = await asyncio.wait_for(client.wait_for_flow(missing_id, timedelta(seconds=25)), 30)
                assert missing.status == dex.FlowStatus.FAILED
                assert missing.error_message is not None and "notFound" in missing.error_message
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
