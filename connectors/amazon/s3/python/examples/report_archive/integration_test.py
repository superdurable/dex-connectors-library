# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the checked-in archive Flow through a real Dex Worker and Client."""

import asyncio
import hashlib
import json
import os
import tempfile
import threading
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, unquote, urlsplit
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.amazon_s3 import new_project_connection

from .flow import CONNECTION_NAME, ReportArchiveFlow, ReportArchiveInput, ReportArchiveOutput


@pytest.mark.asyncio
async def test_report_archive_real_dex(monkeypatch: pytest.MonkeyPatch) -> None:
    """Verify retry, create-only read-back, all four Steps, and unwired rejection."""
    dex_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    objects: dict[str, tuple[bytes, str]] = {}
    requests: list[str] = []
    drop_first_put = True

    class Provider(BaseHTTPRequestHandler):
        def log_message(self, _format: str, *args: object) -> None:
            """Keep request details out of test logs."""

        def send(self, status: int, body: bytes = b"", headers: dict[str, str] | None = None) -> None:
            """Return one bounded S3 fixture response."""
            self.send_response(status)
            for name, value in (headers or {}).items():
                self.send_header(name, value)
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(body)

        def handle_request(self) -> None:
            """Store or retrieve one report using the S3 REST shape."""
            nonlocal drop_first_put
            location = urlsplit(self.path)
            key = unquote(location.path.removeprefix("/acme-reports/"))
            is_list = "list-type" in parse_qs(location.query)
            if is_list:
                key = parse_qs(location.query).get("prefix", [""])[0]
            requests.append(f"{self.command} {'list' if is_list else key}")
            size = int(self.headers.get("Content-Length", "0"))
            body = self.rfile.read(size) if size else b""
            if not self.headers.get("Authorization", "").startswith(
                "AWS4-HMAC-SHA256 Credential=AKIAS3CONNECTORTEST1/"
            ):
                self.send(403)
                return
            if self.headers.get("x-amz-content-sha256") != hashlib.sha256(body).hexdigest():
                self.send(400)
                return
            if key.startswith("denied/"):
                self.send(403, b"<Error><Code>AccessDenied</Code></Error>")
                return
            if self.command == "PUT":
                if key in objects and self.headers.get("If-None-Match") == "*":
                    self.send(412)
                    return
                objects[key] = (body, self.headers.get("x-amz-meta-dex-idempotency-key", ""))
                if drop_first_put:
                    drop_first_put = False
                    self.connection.shutdown(2)
                    return
                self.send(200, headers={"ETag": '"saved"'})
                return
            if self.command in ("HEAD", "GET") and not is_list:
                if key not in objects:
                    self.send(404)
                    return
                content, marker = objects[key]
                self.send(
                    200,
                    content,
                    {
                        "Content-Length": str(len(content)),
                        "Last-Modified": "Sun, 25 Oct 2026 09:00:00 GMT",
                        "Content-Type": "text/plain; charset=utf-8",
                        "ETag": '"saved"',
                        "X-Amz-Meta-Dex-Idempotency-Key": marker,
                    },
                )
                return
            if self.command == "GET" and is_list:
                if key in objects:
                    contents = f"<Contents><Key>{key}</Key><Size>{len(objects[key][0])}</Size></Contents>"
                else:
                    contents = ""
                xml = f"<ListBucketResult><IsTruncated>false</IsTruncated>{contents}</ListBucketResult>"
                self.send(200, xml.encode())
                return
            self.send(400)

        do_GET = handle_request
        do_HEAD = handle_request
        do_PUT = handle_request

    provider = ThreadingHTTPServer(("127.0.0.1", 0), Provider)
    provider_thread = threading.Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    try:
        with tempfile.TemporaryDirectory(prefix="s3-python-dex-") as directory:
            config_path = Path(directory) / "connections.json"
            config_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "amazon-s3",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/"
                                    "connectors/amazon/s3"
                                ),
                                "provider": "amazon-s3",
                                "authMethodId": "api-key-only",
                                "configuration": {
                                    "region": "eu-west-1",
                                    "endpoint": f"http://127.0.0.1:{provider.server_port}",
                                    "defaultBucket": "acme-reports",
                                },
                                "credentials": {
                                    "access_key_id": "AKIAS3CONNECTORTEST1",
                                    "secret_access_key": "s3-connector-test/secret+value",
                                },
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            config_path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(config_path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(config_path)})
            connection = new_project_connection(project, CONNECTION_NAME)
            flow = ReportArchiveFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=dex_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=15)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=dex_address)
                ) as client:
                    success_id = f"s3-python-success-{uuid4().hex}"
                    await client.start_flow(
                        flow, success_id, ReportArchiveInput("acme-reports", "reports/weekly.md", "hello\n")
                    )
                    success = await asyncio.wait_for(
                        client.wait_for_flow(success_id, timedelta(seconds=45)), 60
                    )
                    assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                    output = success.single_output(ReportArchiveOutput)
                    assert output.stored.is_from_earlier_attempt
                    assert output.metadata.size_bytes == 6
                    assert output.text.text == "hello\n"
                    assert output.listing.objects[0].key == "reports/weekly.md"
                    assert requests[:6] == [
                        "PUT reports/weekly.md",
                        "PUT reports/weekly.md",
                        "HEAD reports/weekly.md",
                        "HEAD reports/weekly.md",
                        "GET reports/weekly.md",
                        "GET list",
                    ]
                    denied_id = f"s3-python-denied-{uuid4().hex}"
                    await client.start_flow(
                        flow, denied_id, ReportArchiveInput("acme-reports", "denied/report.md", "blocked")
                    )
                    denied = await asyncio.wait_for(
                        client.wait_for_flow(denied_id, timedelta(seconds=30)), 45
                    )
                    assert denied.status == dex.FlowStatus.FAILED
                    assert denied.error_message and "providerRejected" in denied.error_message
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
