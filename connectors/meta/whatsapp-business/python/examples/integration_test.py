# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for WhatsApp Business Flows."""

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

from dex_connectors.meta_whatsapp_business import (
    Client,
    Config,
    Credentials,
    ListPhoneNumbersInput,
    PhoneNumbersPage,
    SendTemplateMessageInput,
    SendTextMessageInput,
    SentMessage,
    new_connection,
)

from .phone_numbers.flow import PhoneNumbersFlow
from .template_message.flow import TemplateMessageFlow
from .text_message.flow import TextMessageFlow


@pytest.mark.asyncio
async def test_whatsapp_flows_retry_and_uncertainty(tmp_path: Path) -> None:
    """Run all three Flows across real Dex Worker and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    text_requests = 0
    query_requests = 0
    template_requests = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal text_requests, query_requests, template_requests
        assert request.headers["Authorization"] == "Bearer synthetic-system-user-token"
        if request.method == "GET":
            assert request.url.path == "/123456789/phone_numbers"
            query_requests += 1
            if query_requests == 1:
                return httpx.Response(503)
            return httpx.Response(
                200,
                json={
                    "data": [
                        {
                            "id": "1906385232743451",
                            "verified_name": "Example Store",
                            "display_phone_number": "+1 631-555-5555",
                        }
                    ]
                },
            )
        assert request.method == "POST"
        assert request.url.path == "/1906385232743451/messages"
        payload = json.loads(request.content)
        if payload["type"] == "text":
            text_requests += 1
            if text_requests == 1:
                return httpx.Response(503)
            return httpx.Response(
                200, json={"messaging_product": "whatsapp", "messages": [{"id": "wamid.example-text"}]}
            )
        assert payload["type"] == "template"
        template_requests += 1
        return httpx.Response(500)

    reference = ConnectionRef("whatsapp-business", "whatsapp-business")
    credentials = Credentials(SecretString("synthetic-system-user-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        query_flow = PhoneNumbersFlow(connection)
        text_flow = TextMessageFlow(connection)
        template_flow = TemplateMessageFlow(connection)
        registry = dex.Registry([query_flow, text_flow, template_flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                query_id = f"whatsapp-python-query-{uuid4().hex}"
                await client.start_flow(query_flow, query_id, ListPhoneNumbersInput("123456789", "cursor"))
                query_result = await asyncio.wait_for(
                    client.wait_for_flow(query_id, timedelta(seconds=25)), 30
                )
                assert query_result.status == dex.FlowStatus.COMPLETED, query_result.error_message
                assert query_result.single_output(PhoneNumbersPage).numbers[0].id == "1906385232743451"
                assert query_requests == 2

                text_id = f"whatsapp-python-text-{uuid4().hex}"
                await client.start_flow(
                    text_flow, text_id, SendTextMessageInput("1906385232743451", "16315551234", "Hello")
                )
                text_result = await asyncio.wait_for(client.wait_for_flow(text_id, timedelta(seconds=25)), 30)
                assert text_result.status == dex.FlowStatus.COMPLETED, text_result.error_message
                assert text_result.single_output(SentMessage).message_id == "wamid.example-text"
                assert text_requests == 2

                template_id = f"whatsapp-python-template-{uuid4().hex}"
                await client.start_flow(
                    template_flow,
                    template_id,
                    SendTemplateMessageInput(
                        "1906385232743451", "16315551234", "order_update", "en_US", ("A123",)
                    ),
                )
                template_result = await asyncio.wait_for(
                    client.wait_for_flow(template_id, timedelta(seconds=25)), 30
                )
                assert template_result.status == dex.FlowStatus.FAILED
                assert "uncertain" in (template_result.error_message or "")
                assert template_requests == 1
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
