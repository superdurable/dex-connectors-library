# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client coverage for the Gmail read-and-reply Flow."""

import asyncio
import json
import os
from collections.abc import Callable
from datetime import timedelta
from pathlib import Path
from typing import TypeVar
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    MutationResult,
    SecretString,
    StaticCredentialProvider,
    StepAnnotations,
)

from dex_connectors.google_gmail import (
    Config,
    Connection,
    Credentials,
    GmailProvider,
    SendMessageInput,
    SendMessageOutput,
    new_connection,
    new_send_message_step,
)
from dex_connectors.google_gmail.gmailmock import Mock
from examples.thread_reply.flow import ThreadReplyFlow
from examples.thread_reply.models import CONNECTION_NAME, ThreadReplyInput, ThreadReplyOutput

FlowInput = TypeVar("FlowInput")


async def _run_flow(
    connection: Connection,
    input_value: ThreadReplyInput,
    cache_directory: Path,
) -> dex.FlowResult:
    """Run the checked-in example Flow against a real Dex Worker and Client."""
    return await _run_registered_flow(ThreadReplyFlow(connection), input_value, cache_directory)


async def _run_registered_flow(
    flow: dex.Flow[FlowInput], input_value: FlowInput, cache_directory: Path
) -> dex.FlowResult:
    """Start one unique Flow and wait for its actual terminal Dex result."""
    server_address = os.environ.get("DEX_SERVER_ADDRESS", "localhost:8801")
    blob_cache = dex.open_blob_cache(dex.BlobCacheConfig(str(cache_directory), 64 * 1024 * 1024))
    registry = dex.Registry([flow], allow_async_handlers=True)
    worker = dex.AsyncWorker(registry, blob_cache, dex.WorkerOptions(server_address=server_address))
    client = dex.AsyncClient(registry, blob_cache, dex.ClientOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        flow_id = f"gmail-python-integ-{uuid4().hex}"
        await client.start_flow(flow, flow_id, input_value)
        return await client.wait_for_flow(flow_id, timeout=timedelta(seconds=40))
    finally:
        await client.close()
        await worker.close()
        await worker_task
        blob_cache.close()


def _source_response() -> httpx.Response:
    return httpx.Response(
        200,
        json={
            "id": "source-1",
            "threadId": "thread-1",
            "internalDate": "1772615700000",
            "payload": {
                "headers": [
                    {"name": "From", "value": "Ada <ada@example.com>"},
                    {"name": "Message-ID", "value": "<source@example.com>"},
                    {"name": "Subject", "value": "Question"},
                ]
            },
        },
    )


async def _provider_flow(
    respond: Callable[[httpx.Request], httpx.Response],
    input_value: ThreadReplyInput,
    cache_directory: Path,
) -> dex.FlowResult:
    """Use a fake Gmail HTTP boundary with a real Dex Worker and Client."""
    credential = Credentials(SecretString("inert-access"), "owner@example.org")
    credentials = StaticCredentialProvider({ConnectionRef("google", CONNECTION_NAME): credential})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        provider = GmailProvider(
            Config(endpoint="http://localhost/gmail/v1"), credentials, http_client=http_client
        )
        return await _run_flow(
            new_connection(provider, ConnectionRef("google", CONNECTION_NAME)), input_value,
            cache_directory,
        )


@pytest.mark.asyncio
async def test_real_dex_retries_read_then_sends_one_threaded_reply(tmp_path: Path) -> None:
    """Dex retries the Query Step, then the Mutation Step confirms one reply."""
    requests: list[str] = []

    def respond(request: httpx.Request) -> httpx.Response:
        request_kind = request.url.params.get("format", "POST") if request.method == "GET" else "POST"
        requests.append(request_kind)
        if request_kind == "full" and requests.count("full") == 1:
            return httpx.Response(503)
        if request.method == "GET":
            return _source_response()
        payload = json.loads(request.content)
        assert payload["threadId"] == "thread-1"
        return httpx.Response(200, json={"id": "reply-1", "threadId": "thread-1"})

    result = await _provider_flow(
        respond, ThreadReplyInput("source-1", "Processing complete."), tmp_path / "cache"
    )
    assert result.status == dex.FlowStatus.COMPLETED
    assert result.single_output(ThreadReplyOutput) == ThreadReplyOutput("thread-1", "reply-1")
    assert requests == ["full", "full", "metadata", "POST"]


@pytest.mark.asyncio
async def test_real_dex_unwired_optional_branch_fails_with_failure(tmp_path: Path) -> None:
    """A definite Gmail 404 selects notFound and fails the unwired optional route."""
    result = await _provider_flow(
        lambda _: httpx.Response(404),
        ThreadReplyInput("missing-1", "Processing complete."),
        tmp_path / "cache",
    )
    assert result.status == dex.FlowStatus.FAILED
    assert result.error_message is not None
    assert "notFound" in result.error_message


@pytest.mark.asyncio
async def test_real_dex_manifest_mocks_run_the_same_flow(tmp_path: Path) -> None:
    """Manifest default Query and Mutation cases complete through the two Step factories."""
    mock = Mock(CONNECTION_NAME)
    result = await _run_flow(
        mock.connection, ThreadReplyInput("source-1", "Processing complete."), tmp_path / "cache"
    )
    assert result.status == dex.FlowStatus.COMPLETED
    assert result.single_output(ThreadReplyOutput) == ThreadReplyOutput(
        "18c0a1b2c3d4e5f0", "18c0a1b2c3d4e700"
    )
    assert len(mock.get_message.calls()) == 1
    assert len(mock.reply_to_message.calls()) == 1


# dex:group group-id:gmail group-label:"Gmail"
# dex:explanation text:"Complete after Gmail confirms one new message was sent."
class SendConfirmed(dex.Step[MutationResult[SendMessageOutput]]):
    """Complete after the connector proves a send was accepted."""

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: MutationResult[SendMessageOutput]
    ) -> dex.StepDecision:
        """Return only Gmail's message ID."""
        return dex.graceful_complete(input.value.message_id)


class SendMessageFlow(dex.Flow[SendMessageInput]):
    """Exercise the third Gmail operation-specific Step factory on real Dex."""

    def __init__(self, connection: Connection) -> None:
        self.confirmed = SendConfirmed()
        self.send = new_send_message_step(
            step_type="SendOneEmail",
            step_input_type=SendMessageInput,
            annotations=StepAnnotations(
                group_id="gmail", group_label="Gmail", explanation="Send one message after read-back."
            ),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            sent=self.confirmed,
        )

    def get_steps(self) -> dex.StepList[SendMessageInput]:
        """Register sendMessage and its happy-path completion target."""
        return dex.StepList.start_step(self.send).other_steps(self.confirmed)


@pytest.mark.asyncio
async def test_real_dex_send_retry_reads_back_before_duplicate_post(tmp_path: Path) -> None:
    """An ambiguous first POST retries, then finds the sent message before another POST."""
    methods: list[str] = []

    def respond(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        if request.method == "POST":
            return httpx.Response(500)
        if methods.count("GET") <= 4:
            return httpx.Response(200, json={"messages": []})
        return httpx.Response(200, json={"messages": [{"id": "sent-1", "threadId": "thread-1"}]})

    credential = Credentials(SecretString("inert-access"), "owner@example.org")
    credentials = StaticCredentialProvider({ConnectionRef("google", CONNECTION_NAME): credential})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        provider = GmailProvider(
            Config(endpoint="http://localhost/gmail/v1"), credentials, http_client=http_client
        )
        flow = SendMessageFlow(new_connection(provider, ConnectionRef("google", CONNECTION_NAME)))
        assert flow.send.get_step_options().execute_durability == dex.StepDurability.ASYNC
        result = await _run_registered_flow(
            flow,
            SendMessageInput(["ada@example.com"], "Progress", "Processing complete."),
            tmp_path / "cache",
        )
    assert result.status == dex.FlowStatus.COMPLETED, (result.error_message, methods)
    assert result.single_output(str) == "sent-1"
    assert methods == ["POST", "GET", "GET", "GET", "GET", "GET"]


@pytest.mark.asyncio
async def test_real_dex_503_resends_after_sent_mail_miss(tmp_path: Path) -> None:
    """A definite 503 retries with one pre-send search before the second POST."""
    methods: list[str] = []

    def respond(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        if request.method == "GET":
            return httpx.Response(200, json={"messages": []})
        if methods.count("POST") == 1:
            return httpx.Response(503)
        return httpx.Response(200, json={"id": "sent-after-503", "threadId": "thread-1"})

    credential = Credentials(SecretString("inert-access"), "owner@example.org")
    credentials = StaticCredentialProvider({ConnectionRef("google", CONNECTION_NAME): credential})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        provider = GmailProvider(
            Config(endpoint="http://localhost/gmail/v1"), credentials, http_client=http_client
        )
        flow = SendMessageFlow(new_connection(provider, ConnectionRef("google", CONNECTION_NAME)))
        assert flow.send.get_step_options().execute_durability == dex.StepDurability.ASYNC
        result = await _run_registered_flow(
            flow,
            SendMessageInput(["ada@example.com"], "Progress", "Processing complete."),
            tmp_path / "cache",
        )
    assert result.status == dex.FlowStatus.COMPLETED, (result.error_message, methods)
    assert result.single_output(str) == "sent-after-503"
    assert methods == ["POST", "GET", "GET", "GET", "GET", "GET", "POST"]
