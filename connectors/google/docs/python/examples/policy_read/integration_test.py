# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the checked-in Flow with a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.google_docs import Client, Config, Credentials, GetDocumentTextOutput, new_connection

from .flow import CONNECTION_NAME, PolicyReadFlow, PolicyReadRequest


@pytest.mark.asyncio
async def test_read_retries_and_unwired_not_found_fails(tmp_path: Path) -> None:
    """Verify the read's retry and optional branch through real Dex state."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/missing"):
            return httpx.Response(404, json={"error": {"code": 404}})
        if len(requests) == 1:
            return httpx.Response(503, json={"error": {"code": 503}})
        return httpx.Response(200, json={
            "documentId": "policy123",
            "title": "Policy",
            "revisionId": "rev-1",
            "tabs": [{
                "tabProperties": {"tabId": "t.0"},
                "documentTab": {"body": {"content": [
                    {"endIndex": 1, "sectionBreak": {}},
                    {"startIndex": 1, "endIndex": 8, "paragraph": {
                        "elements": [{"textRun": {"content": "Policy\n"}}]
                    }},
                ]}},
            }],
        })

    reference = ConnectionRef("google", CONNECTION_NAME)
    credentials = StaticCredentialProvider({
        reference: Credentials(access_token=SecretString("inert-docs-token"))
    })
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        provider = Client(
            Config(docs_endpoint="http://127.0.0.1:44350", drive_endpoint="http://127.0.0.1:44350"),
            credentials,
            provider_http_client,
        )
        connection = new_connection(provider, reference)
        flow = PolicyReadFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=20)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=address)
            ) as client:
                successful_id = f"google-docs-python-{uuid4().hex}"
                await client.start_flow(flow, successful_id, PolicyReadRequest("policy123"))
                successful = await asyncio.wait_for(
                    client.wait_for_flow(successful_id, timedelta(seconds=20)), 30
                )
                assert successful.status == dex.FlowStatus.COMPLETED, successful.error_message
                output = successful.single_output(GetDocumentTextOutput)
                assert output.document_id == "policy123"
                assert output.text == "Policy"
                assert len(requests) == 2

                missing_id = f"google-docs-python-{uuid4().hex}"
                await client.start_flow(flow, missing_id, PolicyReadRequest("missing"))
                missing = await asyncio.wait_for(
                    client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                )
                assert missing.status == dex.FlowStatus.FAILED
                assert missing.error_message is not None
                assert "notFound" in missing.error_message
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
