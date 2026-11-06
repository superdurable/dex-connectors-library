# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Forms example through a real Dex Worker and local Forms stand-in."""

import asyncio
import json
import os
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Thread
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.google_forms import Client, Config, Credentials, FormResponse, new_connection

from .flow import CONNECTION_NAME, RecorderInput, ResponseRecorderFlow


@pytest.mark.asyncio
async def test_response_recorder_reads_all_operations_in_real_dex(tmp_path: Path) -> None:
    """Verify Step transitions and provider calls against a real Dex service."""
    service_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[str] = []

    class FormsHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            requests.append(self.path)
            if self.path == "/v1/forms/form_1":
                body = {"formId": "form_1", "info": {"title": "Vendor intake"}, "items": []}
            elif self.path == "/v1/forms/form_1/responses?pageSize=25":
                body = {
                    "responses": [
                        {
                            "responseId": "response_1",
                            "lastSubmittedTime": "2026-09-30T09:00:00.123456789Z",
                        }
                    ]
                }
            elif self.path == "/v1/forms/form_1/responses/response_1":
                body = {
                    "formId": "form_1",
                    "responseId": "response_1",
                    "lastSubmittedTime": "2026-09-30T09:00:00.123456789Z",
                }
            else:
                self.send_error(404)
                return
            content = json.dumps(body).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(content)))
            self.end_headers()
            self.wfile.write(content)

        def log_message(self, format: str, *args: object) -> None:
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), FormsHandler)
    thread = Thread(target=server.serve_forever, daemon=True)
    thread.start()
    connection_ref = ConnectionRef("google", CONNECTION_NAME)
    credential_source = StaticCredentialProvider(
        {connection_ref: Credentials(access_token=SecretString("inert-token"))}
    )
    client = Client(Config(endpoint=f"http://127.0.0.1:{server.server_port}"), credential_source)
    connection = new_connection(client, connection_ref)
    flow = ResponseRecorderFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=service_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=service_address)
        ) as dex_client:
            flow_id = f"google-forms-python-{uuid4()}"
            await dex_client.start_flow(flow, flow_id, RecorderInput("form_1"))
            result = await dex_client.wait_for_flow(flow_id, timedelta(seconds=30))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            assert result.single_output(FormResponse).response_id == "response_1"
            assert result.single_output(FormResponse).last_submitted_at == "2026-09-30T09:00:00.123456789Z"
            assert requests == [
                "/v1/forms/form_1",
                "/v1/forms/form_1/responses?pageSize=25",
                "/v1/forms/form_1/responses/response_1",
            ]
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await connection.aclose()
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
