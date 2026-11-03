# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run both Snowflake examples across a real Dex Worker and Client."""

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

from dex_connectors.snowflake import (
    CancelStatementInput,
    Client,
    Config,
    Credentials,
    StatementCancellation,
    StatementResult,
    SubmitStatementInput,
    new_connection,
)

from .cancel_flow import CancelStatementFlow
from .flow import CONNECTION_NAME, StatementResultFlow

HANDLE = "00000000-0000-4000-8000-000000000003"


@pytest.mark.asyncio
async def test_submit_read_and_cancel_in_real_dex() -> None:
    """Verify Step routing and provider calls through the running Dex stack."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path == "/api/v2/statements" and request.method == "POST":
            return httpx.Response(202, json={"statementHandle": HANDLE})
        if request.url.path == f"/api/v2/statements/{HANDLE}" and request.method == "GET":
            return httpx.Response(
                200,
                json={
                    "statementHandle": HANDLE,
                    "resultSetMetaData": {
                        "format": "jsonv2",
                        "numRows": 1,
                        "partitionInfo": [{}],
                        "rowType": [
                            {"name": "EVENT_COUNT", "type": "fixed", "precision": 18, "nullable": False}
                        ],
                    },
                    "data": [["128"]],
                },
            )
        if request.url.path == f"/api/v2/statements/{HANDLE}/cancel" and request.method == "POST":
            return httpx.Response(200, json={"statementHandle": HANDLE})
        return httpx.Response(404, json={"code": "000709", "sqlState": "02000"})

    reference = ConnectionRef("snowflake", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        connection = new_connection(
            Client(
                Config(account_identifier="myorg-analytics"),
                StaticCredentialProvider(
                    {
                        reference: Credentials(
                            auth_method_id="programmatic-access-token",
                            programmatic_access_token=SecretString("inert-test-token"),
                        ),
                    }
                ),
                provider_http_client,
                local_provider_url="http://127.0.0.1",
            ),
            reference,
        )
        statement_flow = StatementResultFlow(connection)
        cancel_flow = CancelStatementFlow(connection)
        registry = dex.Registry([statement_flow, cancel_flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="snowflake-python-dex-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        submit_id = f"snowflake-python-result-{uuid4().hex}"
                        await dex_client.start_flow(
                            statement_flow,
                            submit_id,
                            SubmitStatementInput(statement="SELECT ? AS EVENT_COUNT", parameters=("128",)),
                        )
                        result = await asyncio.wait_for(
                            dex_client.wait_for_flow(submit_id, timedelta(seconds=20)), 30
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        read = result.single_output(StatementResult)
                        assert read.statement_handle == HANDLE
                        assert read.rows[0] is not None
                        assert read.rows[0]["EVENT_COUNT"] == "128"

                        cancel_id = f"snowflake-python-cancel-{uuid4().hex}"
                        await dex_client.start_flow(
                            cancel_flow, cancel_id, CancelStatementInput(statement_handle=HANDLE)
                        )
                        cancellation = await asyncio.wait_for(
                            dex_client.wait_for_flow(cancel_id, timedelta(seconds=20)), 30
                        )
                        assert cancellation.status == dex.FlowStatus.COMPLETED, cancellation.error_message
                        assert cancellation.single_output(StatementCancellation).statement_handle == HANDLE
                        assert [request.method for request in requests] == ["POST", "GET", "POST"]
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
