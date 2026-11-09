# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Odoo Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.odoo import (
    Client,
    Config,
    Credentials,
    Partner,
    PartnerSearchPage,
    SearchPartnersInput,
    new_connection,
)

from .flow import CONNECTION_NAME, PartnerSearchFlow


@pytest.mark.asyncio
async def test_partner_search_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.raw_path == b"/json/2/res.partner/search_read"
        assert request.headers["Authorization"] == "Bearer sample-odoo-api-key"
        assert request.headers["X-Odoo-Database"] == "mycompany"
        assert (
            request.content == b'{"domain":[["is_company","=",true],["name","ilike","%Acme%"]],'
            b'"fields":["id","name"],"limit":5}'
        )
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json=[{"id": 42, "name": "Acme Company"}],
        )

    reference = ConnectionRef("odoo", CONNECTION_NAME)
    credentials = Credentials(SecretString("sample-odoo-api-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345", database="mycompany"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = PartnerSearchFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"odoo-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, SearchPartnersInput(name_pattern="%Acme%", limit=5))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(PartnerSearchPage) == PartnerSearchPage(
                    (Partner(42, "Acme Company"),)
                )
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
