# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Firestore Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.google_firestore import (
    Client,
    Config,
    Credentials,
    DocumentsPage,
    ListDocumentsInput,
    new_connection,
)

from .flow import CONNECTION_NAME, DocumentsFlow


@pytest.mark.asyncio
async def test_documents_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Bearer synthetic-google-firestore-token"
        assert request.url.path == "/v1/projects/sample-project/databases/(default)/documents/events"
        assert request.url.params["pageSize"] == "100"
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "documents": [
                    {"name": "projects/sample-project/databases/(default)/documents/events/sample_document"}
                ],
                "nextPageToken": None,
            },
        )

    reference = ConnectionRef("google-firestore", CONNECTION_NAME)
    credentials = Credentials(
        "synthetic-client",
        SecretString("synthetic-secret"),
        SecretString("synthetic-google-firestore-token"),
        SecretString("synthetic-refresh"),
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = DocumentsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"google_firestore-python-{uuid4().hex}"
                await client.start_flow(
                    flow, flow_id, ListDocumentsInput(project_id="sample-project", collection_id="events")
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                expected_name = "projects/sample-project/databases/(default)/documents/events/sample_document"
                assert result.single_output(DocumentsPage).documents[0].name == expected_name
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
