# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Dropbox Sign Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.dropbox_sign import (
    Client,
    Config,
    Credentials,
    GetSignatureRequestInput,
    SignatureRequestStatus,
    new_connection,
)

from .flow import CONNECTION_NAME, SignatureRequestInspectionFlow


@pytest.mark.asyncio
async def test_signature_request_inspection_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    request_id = "fa5c8a0b0f492d768749333ad6fcc214c111e967"
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == f"/v3/signature_request/{request_id}"
        assert request.headers["Authorization"].startswith("Basic ")
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "signature_request": {
                    "signature_request_id": request_id,
                    "title": "Example agreement",
                    "is_complete": True,
                    "is_declined": False,
                    "has_error": False,
                },
            },
        )

    reference = ConnectionRef("dropbox-sign", CONNECTION_NAME)
    credentials = Credentials(SecretString("sample-dropbox-sign-api-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345/v3"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = SignatureRequestInspectionFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"dropbox_sign-python-{uuid4().hex}"
                await client.start_flow(
                    flow,
                    flow_id,
                    GetSignatureRequestInput(signature_request_id=request_id),
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(SignatureRequestStatus).id == request_id
                assert result.single_output(SignatureRequestStatus).is_complete
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
