# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for Weiban listTags."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.weiban import (
    Client,
    Config,
    Credentials,
    ListTagsInput,
    TagsResult,
    new_connection,
)

from .flow import CONNECTION_NAME, ListTagsFlow


@pytest.mark.asyncio
async def test_list_tags_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    token_body = '{"access_token":"REDACTED_WEIBAN_ACCESS_TOKEN","errcode":0,"expires_in":7200}'
    tag_body = (
        '{"errcode":0,"total":1,"tag_list":'
        '[{"tag_id":"tag_123","tag_name":"Returning","tag_group":null}]}'
    )

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200, text=token_body if request.url.path.endswith("access_token/get") else tag_body
        )

    reference = ConnectionRef("weiban", CONNECTION_NAME)
    credentials = Credentials("corp_test", SecretString("REDACTED_WEIBAN_SECRET"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(
            Config(),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider_client, reference)
        flow = ListTagsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"weiban-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListTagsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(TagsResult)
                assert output.tags and output.tags[0].group == ""
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
