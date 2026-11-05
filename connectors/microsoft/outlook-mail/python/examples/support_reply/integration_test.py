# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise Outlook Mail Steps through a real Dex Worker and Client."""

import asyncio
import tempfile
from collections import Counter
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.microsoft_outlook_mail import Client, Config, Credentials, new_connection

from .flow import CONNECTION_NAME, SupportOutcome, SupportReplyFlow, SupportRequest

SOURCE_ID = "AAMkAGI2MDEyMzQ1SOURCE001"
DRAFT_ID = "AAMkAGI2MDEyMzQ1DRAFT001"
FOLDER_ID = "AAMkAGI2MDEyMzQ1FOLDERARCH"


@pytest.mark.asyncio
async def test_support_reply_and_new_message_in_real_dex() -> None:
    """Cover all six operations, one retry, and an unwired refusal."""
    address = "jane@acme.example.com"
    requests: list[httpx.Request] = []
    search_attempts: Counter[str] = Counter()

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        path = request.url.path
        query = request.url.query.decode()
        if request.method == "GET" and path.endswith("/mailFolders/inbox/messages"):
            if "reject%40acme.example.com" in query:
                return httpx.Response(403, json={"error": {"code": "ErrorAccessDenied"}})
            if "new%40acme.example.com" in query:
                return httpx.Response(200, json={"value": []})
            search_attempts["reply"] += 1
            if search_attempts["reply"] == 1:
                return httpx.Response(
                    429, headers={"Retry-After": "1"}, json={"error": {"code": "TooManyRequests"}}
                )
            return httpx.Response(
                200,
                json={
                    "value": [
                        {
                            "id": SOURCE_ID,
                            "subject": "Refund request",
                            "receivedDateTime": "2026-09-28T09:00:00Z",
                            "from": {"emailAddress": {"address": address}},
                            "parentFolderId": "inbox",
                        }
                    ]
                },
            )
        if request.method == "GET" and path.endswith("/messages"):
            return httpx.Response(200, json={"value": []})
        if request.method == "GET" and path.endswith("/messages/" + SOURCE_ID):
            if "id%2CisRead" in query or "id,isRead" in query:
                return httpx.Response(200, json={"id": SOURCE_ID, "isRead": False})
            return httpx.Response(
                200,
                json={
                    "id": SOURCE_ID,
                    "subject": "Refund request",
                    "receivedDateTime": "2026-09-28T09:00:00Z",
                    "parentFolderId": "inbox",
                    "body": {"contentType": "text", "content": "Please help."},
                },
            )
        if request.method == "POST" and path.endswith("/createReply"):
            return httpx.Response(
                201,
                json={
                    "id": DRAFT_ID,
                    "isDraft": True,
                    "subject": "RE: Refund request",
                    "toRecipients": [{"emailAddress": {"address": address}}],
                },
            )
        if request.method == "POST" and path.endswith("/messages"):
            return httpx.Response(
                201,
                json={
                    "id": DRAFT_ID,
                    "isDraft": True,
                    "subject": "Your refund request",
                    "toRecipients": [{"emailAddress": {"address": "new@acme.example.com"}}],
                },
            )
        if request.method == "PATCH" and path.endswith("/messages/" + DRAFT_ID):
            return httpx.Response(200, json={})
        if request.method == "POST" and path.endswith("/messages/" + DRAFT_ID + "/send"):
            return httpx.Response(202)
        if request.method == "PATCH" and path.endswith("/messages/" + SOURCE_ID):
            return httpx.Response(200, json={})
        if request.method == "GET" and path.endswith("/mailFolders/archive"):
            return httpx.Response(200, json={"id": FOLDER_ID, "displayName": "Archive"})
        if request.method == "POST" and path.endswith("/messages/" + SOURCE_ID + "/move"):
            return httpx.Response(200, json={"id": SOURCE_ID, "parentFolderId": FOLDER_ID})
        raise AssertionError(f"unexpected Graph request: {request.method} {path}")

    connection_ref = ConnectionRef("microsoft", CONNECTION_NAME)
    credentials = Credentials(auth_method_id="microsoft-oauth", access_token=SecretString("INERT_TOKEN"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        provider = Client(
            Config(), StaticCredentialProvider({connection_ref: credentials}), provider_http_client
        )
        connection = new_connection(provider, connection_ref)
        flow = SupportReplyFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="outlook-python-dex-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address="127.0.0.1:40750"))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address="127.0.0.1:40750")
                    ) as client:
                        for customer, expected_action in (
                            (address, "replied"),
                            ("new@acme.example.com", "sentNewMessage"),
                        ):
                            flow_id = f"outlook-python-support-{uuid4().hex}"
                            await client.start_flow(
                                flow, flow_id, SupportRequest(customer, "We can help.", "Your refund request")
                            )
                            result = await asyncio.wait_for(
                                client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                            )
                            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                            assert result.single_output(SupportOutcome).action == expected_action
                        failed_flow_id = f"outlook-python-support-{uuid4().hex}"
                        await client.start_flow(
                            flow,
                            failed_flow_id,
                            SupportRequest("reject@acme.example.com", "We can help.", "Your refund request"),
                        )
                        rejected = await asyncio.wait_for(
                            client.wait_for_flow(failed_flow_id, timedelta(seconds=30)), 40
                        )
                        assert rejected.status == dex.FlowStatus.FAILED
                        assert "providerRejected" in (rejected.error_message or "")
                        assert search_attempts["reply"] == 2
                        assert any(request.url.path.endswith("/move") for request in requests)
                        assert sum(request.url.path.endswith("/send") for request in requests) == 2
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
