# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the Python summary example with a real Dex Worker and Client."""

import asyncio
import json
import os
import tempfile
import threading
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.superdurable_llm import GenerateTextResponse, new_project_connection

from .flow import CONNECTION_NAME, SummarizeTextFlow
from .start import start_summary


@pytest.mark.asyncio
async def test_summary_and_unwired_policy_branch_with_real_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """Check a completed Query and an unwired optional branch across Dex persistence."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[dict[str, object]] = []

    class Provider(BaseHTTPRequestHandler):
        def do_POST(self) -> None:
            body = self.rfile.read(int(self.headers["Content-Length"]))
            requests.append(json.loads(body))
            is_blocked = len(requests) > 1
            chunks = [
                {
                    "id": "chatcmpl-inert",
                    "model": "mock-model",
                    "choices": [
                        {
                            "index": 0,
                            "delta": {"content": "A short summary." if not is_blocked else ""},
                            "finish_reason": None,
                        }
                    ],
                },
                {
                    "id": "chatcmpl-inert",
                    "model": "mock-model",
                    "choices": [
                        {"index": 0, "delta": {}, "finish_reason": "content_filter" if is_blocked else "stop"}
                    ],
                },
            ]
            payload = "".join("data: " + json.dumps(chunk) + "\n\n" for chunk in chunks) + "data: [DONE]\n\n"
            encoded = payload.encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)

        def log_message(self, format: str, *args: object) -> None:
            pass

    provider = ThreadingHTTPServer(("127.0.0.1", 0), Provider)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    try:
        with tempfile.TemporaryDirectory(prefix="llm-python-dex-") as temporary_directory:
            directory = Path(temporary_directory)
            config_file = directory / "connections.json"
            config_file.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "llm",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/"
                                    "connectors/superdurable/llm"
                                ),
                                "provider": "llm",
                                "authMethodId": "api-key-only",
                                "configuration": {
                                    "provider": "openai-compatible",
                                    "baseUrl": f"http://127.0.0.1:{provider.server_port}/v1",
                                    "model": "mock-model",
                                },
                                "credentials": {"api_key": "sk-inert-example"},
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            config_file.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(config_file))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(config_file)})
            connection = new_project_connection(project, CONNECTION_NAME)
            flow = SummarizeTextFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory / "blobs"), 1 << 24))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=15)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=server_address)
                ) as client:
                    input_file = Path(__file__).with_name("sample-input.json")
                    successful_id = f"llm-python-summary-{uuid4().hex}"
                    await start_summary(successful_id, input_file)
                    completed = await asyncio.wait_for(
                        client.wait_for_flow(successful_id, timedelta(seconds=20)), 30
                    )
                    assert completed.status == dex.FlowStatus.COMPLETED, completed.error_message
                    assert completed.single_output(GenerateTextResponse).text == "A short summary."
                    blocked_id = f"llm-python-summary-{uuid4().hex}"
                    await start_summary(blocked_id, input_file)
                    blocked = await asyncio.wait_for(
                        client.wait_for_flow(blocked_id, timedelta(seconds=20)), 30
                    )
                    assert blocked.status == dex.FlowStatus.FAILED
                    assert blocked.error_message and "blocked" in blocked.error_message
                    assert len(requests) == 2
            finally:
                await worker.stop()
                await worker_task
                cache.close()
                await connection.aclose()
                await project.close()
    finally:
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
