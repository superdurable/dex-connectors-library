# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the summarize text Flow with the real createResponse client against a local Dex Server."""

import asyncio
import json
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.openai import CONNECTOR_ID, Client, Config, Credentials, new_connection
from examples.summarize_text.flow import (
    CONNECTION_NAME,
    SummarizeTextFlow,
    SummaryModelConfiguration,
    SummaryOutcome,
    SummaryRequest,
)

RESPONSE_ID = "resp_integration001"


def completed_stream() -> bytes:
    events = [
        {
            "type": "response.created",
            "sequence_number": 0,
            "response": {"id": RESPONSE_ID, "status": "queued"},
        },
        {"type": "response.output_text.delta", "delta": "Shipped "},
        {"type": "response.output_text.delta", "delta": "today."},
        {
            "type": "response.completed",
            "response": {
                "id": RESPONSE_ID,
                "model": "gpt-6-sol",
                "status": "completed",
                "output": [
                    {"type": "message", "content": [{"type": "output_text", "text": "Shipped today."}]}
                ],
                "usage": {"input_tokens": 9, "output_tokens": 3, "total_tokens": 12},
            },
        },
    ]
    return "".join(f"data: {json.dumps(event)}\n\n" for event in events).encode()


class ScriptedOpenAI:
    """Answer POST /responses per Flow from a list of statuses, recording every request."""

    def __init__(self) -> None:
        self.statuses_by_input: dict[str, list[int]] = {}
        self.requests: list[httpx.Request] = []

    def handle(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        statuses = self.statuses_by_input.get(json.loads(request.content)["input"], [])
        status = statuses.pop(0) if statuses else 200
        if status != 200:
            return httpx.Response(status)
        return httpx.Response(200, content=completed_stream(), headers={"Content-Type": "text/event-stream"})


@pytest.mark.asyncio
async def test_summarize_text_real_dex_worker(tmp_path: Path) -> None:
    """Exercise streamed completion, a fenced 503 resend, and an unresent uncertain 500."""
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    provider = ScriptedOpenAI()
    reference = ConnectionRef(CONNECTOR_ID, CONNECTION_NAME)
    openai_client = Client(
        Config(endpoint="http://127.0.0.1:9/v1"),
        StaticCredentialProvider({reference: Credentials(SecretString("sk-integration"))}),
        httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)),
    )
    flow = SummarizeTextFlow(new_connection(openai_client, reference), SummaryModelConfiguration())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 64 * 1024 * 1024))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as client:
            for text, statuses, expected_posts in (
                (f"streamed {uuid4()}", [], 1),
                (f"resend after 503 {uuid4()}", [503], 2),
            ):
                provider.statuses_by_input[text] = list(statuses)
                flow_id = f"openai-python-{uuid4()}"
                await client.start_flow(flow, flow_id, SummaryRequest(text))
                result = await client.wait_for_flow(flow_id, timedelta(seconds=60))
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                outcomes = [
                    item.decode(SummaryOutcome)
                    for item in result.completions
                    if item.step_type == "RecordSummaryOutcome"
                ]
                assert len(outcomes) == 1
                assert outcomes[0].branch == "completed"
                assert outcomes[0].summary == "Shipped today."
                assert outcomes[0].response_id == RESPONSE_ID
                posts = [
                    request for request in provider.requests if json.loads(request.content)["input"] == text
                ]
                assert len(posts) == expected_posts
                assert len({request.headers["X-Client-Request-Id"] for request in posts}) == 1

            uncertain_text = f"uncertain after 500 {uuid4()}"
            provider.statuses_by_input[uncertain_text] = [500, 500]
            uncertain_id = f"openai-python-uncertain-{uuid4()}"
            await client.start_flow(flow, uncertain_id, SummaryRequest(uncertain_text))
            uncertain = await client.wait_for_flow(uncertain_id, timedelta(seconds=60))
            assert uncertain.status == dex.FlowStatus.FAILED
            assert uncertain.error_message is not None and "uncertain" in uncertain.error_message
            assert [json.loads(request.content)["input"] for request in provider.requests].count(
                uncertain_text
            ) == 1
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await openai_client.aclose()
