# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Amazon SNS Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.amazon_sns import (
    Client,
    Config,
    Credentials,
    ListTopicsInput,
    TopicPage,
    new_connection,
)

from .flow import CONNECTION_NAME, TopicListingFlow

TOPIC_RESPONSE = (
    '<ListTopicsResponse xmlns="https://sns.amazonaws.com/doc/2010-03-31/">'
    "<ListTopicsResult><Topics><member>"
    "<TopicArn>arn:aws:sns:eu-west-1:123456789012:alerts</TopicArn>"
    "</member></Topics></ListTopicsResult></ListTopicsResponse>"
)


@pytest.mark.asyncio
async def test_topic_listing_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.path == "/"
        assert "sns/aws4_request" in request.headers["Authorization"]
        assert request.url.params["Action"] == "ListTopics"
        assert request.url.params["Version"] == "2010-03-31"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            text=TOPIC_RESPONSE,
        )

    reference = ConnectionRef("amazon-sns", CONNECTION_NAME)
    credentials = Credentials(
        SecretString("AKIASNSCONNECTOR"), SecretString("sns-test-secret"), SecretString("")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(endpoint="http://127.0.0.1:12345", region="eu-west-1"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = TopicListingFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"sns-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListTopicsInput(next_token=""))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(TopicPage).topic_ar_ns == (
                    "arn:aws:sns:eu-west-1:123456789012:alerts",
                )
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
