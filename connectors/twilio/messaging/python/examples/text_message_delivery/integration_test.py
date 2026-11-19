# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the checked-in Twilio Flow through a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.twilio_messaging import (
    Client,
    Config,
    Credentials,
    Message,
    SendMessageInput,
    new_connection,
)

from .flow import CONNECTION_NAME, MessageDeliveryFlow


@pytest.mark.asyncio
async def test_message_delivery_real_dex(tmp_path: Path) -> None:
    """Cover send, read, and an unwired uncertain outcome through Dex persistence."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    account = "AC" + "0123456789abcdef" * 2
    sid = "SM" + "fedcba9876543210" * 2
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "POST" and b"uncertain" in request.content:
            return httpx.Response(500, json={})
        return httpx.Response(
            201 if request.method == "POST" else 200,
            json={
                "sid": sid,
                "account_sid": account,
                "from": "+14155550100",
                "to": "+14155550123",
                "status": "queued",
                "direction": "outbound-api",
                "num_segments": "1",
                "num_media": "0",
                "date_created": "Wed, 30 Sep 2026 17:04:05 +0000",
            },
        )

    reference = ConnectionRef("twilio-messaging", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        client = Client(
            Config(
                account_sid=account,
                default_sender="+14155550100",
                endpoint="http://127.0.0.1:24209/2010-04-01",
            ),
            StaticCredentialProvider({reference: Credentials(auth_token=SecretString("inert-twilio-token"))}),
            provider_http_client,
        )
        connection = new_connection(client, reference)
        flow = MessageDeliveryFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as dex_client:
                flow_id = f"twilio-python-delivery-{uuid4().hex}"
                await dex_client.start_flow(
                    flow, flow_id, SendMessageInput(to="+14155550123", body="Your table is ready.")
                )
                completed = await dex_client.wait_for_flow(flow_id, timedelta(seconds=30))
                assert completed.status == dex.FlowStatus.COMPLETED, completed.error_message
                message = completed.single_output(Message)
                assert message.sid == sid
                assert message.status == "queued"
                assert [request.method for request in requests] == ["POST", "GET"]

                uncertain_id = f"twilio-python-uncertain-{uuid4().hex}"
                await dex_client.start_flow(
                    flow, uncertain_id, SendMessageInput(to="+14155550123", body="uncertain")
                )
                failed = await dex_client.wait_for_flow(uncertain_id, timedelta(seconds=30))
                assert failed.status == dex.FlowStatus.FAILED
                assert failed.error_message and "uncertain" in failed.error_message
                assert [request.method for request in requests] == ["POST", "GET", "POST"]
        finally:
            await worker.stop()
            await worker_task
            cache.close()
