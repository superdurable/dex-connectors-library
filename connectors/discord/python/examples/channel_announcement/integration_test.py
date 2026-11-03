# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the runnable announcement through a real Dex Worker and Client."""

import asyncio
import json
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.discord import Message, new_project_connection

from .flow import CONNECTION_NAME, AnnouncementInput, ChannelAnnouncementFlow


@pytest.mark.asyncio
async def test_announcement_retries_and_fails_unwired_rejection() -> None:
    """Exercise durable retry, the stable nonce, and optional branch failure."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    channel_id = "112233445566778899"
    message_id = "998877665544332211"
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        body = json.loads(request.content)
        if body["content"] == "reject":
            return httpx.Response(403, json={"code": 50013, "message": "SENTINEL"})
        if len(requests) == 1:
            return httpx.Response(503, json={"code": 0})
        return httpx.Response(
            200,
            json={
                "id": message_id,
                "channel_id": channel_id,
                "author": {"id": "123456789012345678", "username": "dex-bot", "bot": True},
                "content": body["content"],
                "timestamp": "2026-10-13T20:00:00Z",
                "type": 0,
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        with tempfile.TemporaryDirectory(prefix="discord-python-dex-") as cache_directory:
            connections_path = Path(cache_directory) / "connections.json"
            connections_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "discord",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/connectors/discord"
                                ),
                                "provider": "discord",
                                "configuration": {},
                                "credentials": {"bot_token": "inert.test.token"},
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            connections_path.chmod(0o600)
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(connections_path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_http_client)
            flow = ChannelAnnouncementFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as client:
                        successful_id = f"discord-python-{uuid4().hex}"
                        assert await client.start_flow(
                            flow, successful_id, AnnouncementInput(channel_id, "Release today @everyone")
                        )
                        success = await asyncio.wait_for(
                            client.wait_for_flow(successful_id, timedelta(seconds=20)), 30
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        message = success.single_output(Message)
                        assert message.id == message_id
                        assert len(requests) == 2
                        first = json.loads(requests[0].content)
                        second = json.loads(requests[1].content)
                        assert first["nonce"] == second["nonce"]
                        assert first["enforce_nonce"] is True
                        assert first["allowed_mentions"] == {"parse": []}
                        rejected_id = f"discord-python-{uuid4().hex}"
                        assert await client.start_flow(
                            flow, rejected_id, AnnouncementInput(channel_id, "reject")
                        )
                        rejected = await asyncio.wait_for(
                            client.wait_for_flow(rejected_id, timedelta(seconds=20)), 30
                        )
                        assert rejected.status == dex.FlowStatus.FAILED
                        assert "providerRejected" in (rejected.error_message or "")
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
