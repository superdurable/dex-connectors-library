# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the AcrobatSign Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.adobe_acrobat_sign import (
    AgreementStatus,
    Client,
    Config,
    Credentials,
    GetAgreementInput,
    new_connection,
)

from .flow import CONNECTION_NAME, AgreementInspectionFlow


@pytest.mark.asyncio
async def test_agreement_inspection_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.headers["Authorization"] == "Bearer sample-acrobat-sign-integration-key"
        if len(requests) == 1:
            return httpx.Response(503)
        if request.url.path == "/api/rest/v6/baseUris":
            return httpx.Response(200, json={"apiAccessPoint": "http://127.0.0.1:12345/"})
        assert request.url.path == "/api/rest/v6/agreements/3AAABLblqZNOTREALAGREEMENTID5_BjiH"
        return httpx.Response(
            200,
            json={
                "id": "3AAABLblqZNOTREALAGREEMENTID5_BjiH",
                "name": "Example agreement",
                "status": "SIGNED",
            },
        )

    reference = ConnectionRef("adobe-acrobat-sign", CONNECTION_NAME)
    credentials = Credentials(SecretString("sample-acrobat-sign-integration-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345/api/rest/v6"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = AgreementInspectionFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"adobe-acrobat-sign-python-{uuid4().hex}"
                await client.start_flow(
                    flow, flow_id, GetAgreementInput(agreement_id="3AAABLblqZNOTREALAGREEMENTID5_BjiH")
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(AgreementStatus).id == "3AAABLblqZNOTREALAGREEMENTID5_BjiH"
                assert result.single_output(AgreementStatus).status == "SIGNED"
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
