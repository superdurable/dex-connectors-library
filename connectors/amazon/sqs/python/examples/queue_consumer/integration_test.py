# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the checked-in queue consumer through a real Dex Worker and Client."""

import asyncio
import hashlib
import json
import os
import threading
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.amazon_sqs import ReceivedMessages, ReceiveMessagesInput, new_project_connection

from .flow import CONNECTION_NAME, QueueConsumerFlow


@pytest.mark.asyncio
async def test_queue_consumer_receives_and_deletes_through_dex(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """Check provider calls and persisted completion across the Worker boundary."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    actions: list[str] = []
    message_body = '{"orderId":"1042","event":"paid"}'

    class SQSHandler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:
            action = self.headers["X-Amz-Target"]
            actions.append(action)
            request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            if action == "AmazonSQS.ReceiveMessage":
                assert request["QueueUrl"] == queue_url
                response = {"Messages": [{
                    "MessageId": "order-1042",
                    "ReceiptHandle": "receipt-1042",
                    "Body": message_body,
                    "MD5OfBody": hashlib.md5(message_body.encode(), usedforsecurity=False).hexdigest(),
                    "Attributes": {"ApproximateReceiveCount": "1"},
                }]}
            elif action == "AmazonSQS.DeleteMessage":
                assert request == {"QueueUrl": queue_url, "ReceiptHandle": "receipt-1042"}
                response = {}
            else:
                self.send_error(400)
                return
            encoded = json.dumps(response).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/x-amz-json-1.0")
            self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)

        def log_message(self, format: str, *args: object) -> None:
            """Keep provider requests and credential headers out of test logs."""

    provider = ThreadingHTTPServer(("127.0.0.1", 0), SQSHandler)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    queue_url = f"http://127.0.0.1:{provider.server_port}/123456789012/orders"
    connections_path = tmp_path / "connections.json"
    connections_path.write_text(json.dumps({
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [{
            "connectorId": "amazon-sqs",
            "connectionName": CONNECTION_NAME,
            "modulePath": "github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go",
            "provider": "amazon-sqs",
            "configuration": {"region": "eu-west-1", "endpoint": f"http://127.0.0.1:{provider.server_port}",
                              "defaultQueueUrl": queue_url, "receiveWaitSeconds": 1},
            "credentials": {"access_key_id": "AKIASQSEXAMPLETEST", "secret_access_key": "inert-test-secret"},
        }],
        "triggerBindings": [],
    }))
    connections_path.chmod(0o600)
    monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(connections_path))
    project = None
    connection = None
    cache = None
    worker = None
    worker_task = None
    try:
        project = await load_from_environment()
        connection = new_project_connection(project, CONNECTION_NAME)
        flow = QueueConsumerFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        await worker.wait_pull_registered(timeout=20)
        client_options = dex.ClientOptions(server_address=server_address)
        async with dex.AsyncClient(registry, cache, client_options) as client:
            flow_id = f"amazon-sqs-python-{uuid4().hex}"
            await client.start_flow(flow, flow_id, ReceiveMessagesInput())
            completed = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=20)), 30)
            assert completed.status == dex.FlowStatus.COMPLETED, completed.error_message
            messages = completed.single_output(ReceivedMessages).messages
            assert [message.message_id for message in messages] == ["order-1042"]
    finally:
        if worker is not None:
            await worker.stop()
        if worker_task is not None:
            await worker_task
        if cache is not None:
            cache.close()
        if connection is not None:
            await connection.aclose()
        if project is not None:
            await project.close()
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)
    assert actions == ["AmazonSQS.ReceiveMessage", "AmazonSQS.DeleteMessage"]
