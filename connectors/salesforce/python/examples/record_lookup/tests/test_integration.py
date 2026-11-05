# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real dexcli, Worker, and Client verification for the checked-in Flow."""

import asyncio
import json
import subprocess
import time
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import grpc
import httpx
import pytest
from dex.dexpb import dex_pb2_grpc
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider
from google.protobuf import empty_pb2

from dex_connectors.salesforce import Client, Config, Credentials, GetRecordInput, Record, new_connection
from examples.record_lookup.flow import CONNECTION_NAME, RecordLookupFlow

DEX_PORT = 40820
WEB_PORT = 40821
RECORD_ID = "003RM000001AbcdYAC"


@pytest.fixture(scope="module")
def dex_server(tmp_path_factory: pytest.TempPathFactory) -> str:
    """Start and stop an isolated dexcli dev server on assigned ports."""
    data_dir = tmp_path_factory.mktemp("salesforce-dex")
    process = subprocess.Popen(
        [
            "dexcli",
            "dev",
            "-dex-port",
            str(DEX_PORT),
            "-web-port",
            str(WEB_PORT),
            "-open=false",
            "-zd-data-dir",
            str(data_dir),
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    try:
        deadline = time.monotonic() + 45
        with grpc.insecure_channel(f"127.0.0.1:{DEX_PORT}") as channel:
            service = dex_pb2_grpc.FlowServiceStub(channel)
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise RuntimeError("dexcli dev exited before its gRPC service became ready")
                try:
                    service.GetServerInfo(empty_pb2.Empty(), timeout=0.5)
                    break
                except grpc.RpcError:
                    time.sleep(0.1)
            else:
                raise TimeoutError("dexcli dev did not become ready")
        yield f"127.0.0.1:{DEX_PORT}"
    finally:
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=10)


async def run_example(dex_server: str, cache_dir: Path, response: httpx.Response) -> dex.FlowResult:
    """Run the real example Flow through a Worker with a fake Salesforce boundary."""
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return response

    reference = ConnectionRef("salesforce", CONNECTION_NAME)
    credentials = StaticCredentialProvider(
        {
            reference: Credentials(
                access_token=SecretString("inert-salesforce-access"),
                instance_url="http://127.0.0.1:12345",
            )
        }
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        connection = new_connection(Client(Config(), credentials, http_client), reference)
        flow = RecordLookupFlow(connection)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(cache_dir), 64 * 1024 * 1024))
        registry = dex.Registry([flow], allow_async_handlers=True)
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=dex_server))
        client = dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=dex_server))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            flow_id = f"salesforce-python-integ-{uuid4().hex}"
            await client.start_flow(flow, flow_id, GetRecordInput("Contact", RECORD_ID, ("Id", "Email")))
            result = await client.wait_for_flow(flow_id, timeout=timedelta(seconds=40))
        finally:
            await client.close()
            await worker.close()
            await worker_task
            cache.close()
    assert len(requests) == 1
    assert requests[0].url.path == f"/services/data/v62.0/sobjects/Contact/{RECORD_ID}"
    return result


@pytest.mark.asyncio
async def test_real_dex_record_lookup(dex_server: str, tmp_path: Path) -> None:
    """Complete the checked-in Flow after Salesforce returns one Contact."""
    body = {"attributes": {"type": "Contact"}, "Id": RECORD_ID, "Email": "priya@example.com"}
    result = await run_example(dex_server, tmp_path / "cache", httpx.Response(200, content=json.dumps(body)))
    assert result.status == dex.FlowStatus.COMPLETED, result.error_message
    assert result.single_output(Record) == Record(
        "Contact", RECORD_ID, {"Id": RECORD_ID, "Email": "priya@example.com"}
    )


@pytest.mark.asyncio
async def test_real_dex_unwired_not_found_fails(dex_server: str, tmp_path: Path) -> None:
    """Fail the Flow when the optional notFound route has no business reaction."""
    result = await run_example(
        dex_server, tmp_path / "cache-missing", httpx.Response(404, json=[{"errorCode": "NOT_FOUND"}])
    )
    assert result.status == dex.FlowStatus.FAILED
    assert result.error_message is not None and "notFound" in result.error_message
