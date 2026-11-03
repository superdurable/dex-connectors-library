# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the contact read example through a real Dex Worker and Client."""

import asyncio
import hashlib
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

from dex_connectors.intuit_mailchimp import GetMemberInput, Member, new_project_connection

from .flow import CONNECTION_NAME, GetMemberFlow
from .start import start_get_member


@pytest.mark.asyncio
async def test_get_member_retries_and_unwired_not_found_fails(monkeypatch: pytest.MonkeyPatch) -> None:
    """Check retry and optional branch routing across the real Dex boundary."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    subscriber_hash = hashlib.md5(b"ben@example.com").hexdigest()

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(429, headers={"Retry-After": "1"})
        if hashlib.md5(b"absent@example.com").hexdigest() in request.url.path:
            return httpx.Response(404, json={"title": "Resource Not Found"})
        if hashlib.md5(b"malformed@example.com").hexdigest() in request.url.path:
            return httpx.Response(
                200,
                json={
                    "id": hashlib.md5(b"malformed@example.com").hexdigest(),
                    "email_address": "malformed@example.com",
                    "status": "subscribed",
                    "tags": ["invalid tag object"],
                },
            )
        return httpx.Response(
            200,
            json={
                "id": subscriber_hash,
                "email_address": "ben@example.com",
                "status": "subscribed",
                "list_id": "57afe96172",
                "tags_count": 1,
                "tags": [{"name": "vip"}],
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        with tempfile.TemporaryDirectory(prefix="mailchimp-python-dex-") as directory:
            connections_path = Path(directory) / "connections.json"
            connections_path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                        "connections": [
                            {
                                "connectorId": "mailchimp",
                                "connectionName": CONNECTION_NAME,
                                "modulePath": (
                                    "github.com/superdurable/dex-connectors-library/"
                                    "connectors/intuit/mailchimp"
                                ),
                                "provider": "mailchimp",
                                "configuration": {},
                                "credentials": {"api_key": "mailchimpfixturekey-us6"},
                            }
                        ],
                        "triggerBindings": [],
                    }
                )
            )
            connections_path.chmod(0o600)
            monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(connections_path))
            project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(connections_path)})
            connection = new_project_connection(project, CONNECTION_NAME, provider_http_client)
            flow = GetMemberFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as client:
                        successful_id = f"mailchimp-contact-{uuid4().hex}"
                        await client.start_flow(
                            flow, successful_id, GetMemberInput("57afe96172", "ben@example.com")
                        )
                        success = await asyncio.wait_for(
                            client.wait_for_flow(successful_id, timedelta(seconds=20)), 30
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        contact = success.single_output(Member)
                        assert contact.email_address == "ben@example.com"
                        assert contact.tags == ("vip",)
                        assert len(requests) == 2

                        missing_id = f"mailchimp-contact-{uuid4().hex}"
                        missing_path = Path(directory) / "missing.json"
                        missing_path.write_text(
                            json.dumps({"listId": "57afe96172", "emailAddress": "absent@example.com"})
                        )
                        await start_get_member(missing_id, missing_path)
                        missing = await asyncio.wait_for(
                            client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                        )
                        assert missing.status == dex.FlowStatus.FAILED
                        assert "notFound" in (missing.error_message or "")

                        malformed_id = f"mailchimp-contact-{uuid4().hex}"
                        await client.start_flow(
                            flow, malformed_id, GetMemberInput("57afe96172", "malformed@example.com")
                        )
                        malformed = await asyncio.wait_for(
                            client.wait_for_flow(malformed_id, timedelta(seconds=20)), 30
                        )
                        assert malformed.status == dex.FlowStatus.FAILED
                        assert "invalidResponse" in (malformed.error_message or "")
                        assert len(requests) == 4

                        invalid_address_id = f"mailchimp-contact-{uuid4().hex}"
                        await client.start_flow(
                            flow, invalid_address_id, GetMemberInput("57afe96172", "plainstring")
                        )
                        invalid_address = await asyncio.wait_for(
                            client.wait_for_flow(invalid_address_id, timedelta(seconds=20)), 30
                        )
                        assert invalid_address.status == dex.FlowStatus.FAILED
                        assert "defect" in (invalid_address.error_message or "")
                        assert len(requests) == 4
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
                await project.close()
