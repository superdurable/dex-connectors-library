# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the contact Flow through a real Dex Worker and Client."""

import asyncio
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.google_spreadsheet import Client, Config, Credentials, UpsertRowOutput, new_connection

from .flow import CONNECTION_NAME, ContactFlow, ContactInput


@pytest.mark.asyncio
async def test_contact_upsert_retries_once_and_fails_conflict_in_real_dex() -> None:
    """Verify durable retry, stable correlation key, and optional-branch failure."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    writes: list[httpx.Request] = []
    show_duplicate = False

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            values = [["email", "name", "status"]]
            if show_duplicate:
                values.extend(
                    [
                        ["ada@example.test", "Ada", "active"],
                        ["ada@example.test", "Other", "active"],
                    ]
                )
            return httpx.Response(
                200,
                json={
                    "range": "'Contacts'!A1:C1",
                    "majorDimension": "ROWS",
                    "values": values,
                },
            )
        writes.append(request)
        if len(writes) == 1:
            return httpx.Response(503)
        return httpx.Response(200, json={"updates": {"updatedRange": "'Contacts'!A2:C2"}})

    reference = ConnectionRef("google", CONNECTION_NAME)
    credentials = Credentials(
        "client.apps.googleusercontent.com",
        SecretString("secret"),
        SecretString("inert-token"),
        SecretString("refresh"),
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_transport:
        client = Client(
            Config(endpoint="http://127.0.0.1/v4"),
            StaticCredentialProvider({reference: credentials}),
            provider_transport,
        )
        connection = new_connection(client, reference)
        flow = ContactFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="google-sheets-python-dex-") as temporary_directory:
            cache = dex.open_blob_cache(
                dex.BlobCacheConfig(str(Path(temporary_directory) / "blobs"), 1 << 24)
            )
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        flow_id = f"sheets-python-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow,
                            flow_id,
                            ContactInput("sheet-id", "Contacts", "ada@example.test", "Ada", "active"),
                        )
                        result = await asyncio.wait_for(
                            dex_client.wait_for_flow(flow_id, timedelta(seconds=25)), 35
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        output = result.single_output(UpsertRowOutput)
                        assert output.action == "inserted" and output.row_number == 2
                        assert len(writes) == 2
                        assert (
                            writes[0].headers["X-Idempotency-Key"] == writes[1].headers["X-Idempotency-Key"]
                        )
                        assert writes[0].headers["X-Idempotency-Key"]

                        show_duplicate = True
                        conflict_id = f"sheets-python-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow,
                            conflict_id,
                            ContactInput("sheet-id", "Contacts", "ada@example.test", "Ada", "active"),
                        )
                        conflict = await asyncio.wait_for(
                            dex_client.wait_for_flow(conflict_id, timedelta(seconds=20)), 30
                        )
                        assert conflict.status == dex.FlowStatus.FAILED
                        assert "conflict" in (conflict.error_message or "")
                        assert len(writes) == 2
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
