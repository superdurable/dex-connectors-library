# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run both WeCom Mutations through a real Dex Worker and Client."""

import asyncio
import json
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.tencent_wecom import (
    Client,
    Config,
    Credentials,
    SendApplicationMessageInput,
    SendGroupRobotMessageInput,
    SentApplicationMessage,
    SentGroupRobotMessage,
    new_connection,
)

from .application.flow import WeComApplicationFlow
from .group_robot.flow import WeComGroupRobotFlow


@pytest.mark.asyncio
async def test_robot_retry_and_application_delivery(tmp_path: Path) -> None:
    """Verify Fence retry, token exchange, and secret-free persisted results."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    robot_sends = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal robot_sends
        requests.append(request)
        if request.url.path == "/cgi-bin/webhook/send":
            robot_sends += 1
            if robot_sends == 1:
                return httpx.Response(503)
            return httpx.Response(200, json={"errcode": 0, "errmsg": "ok"})
        if request.url.path == "/cgi-bin/gettoken":
            return httpx.Response(
                200, json={"errcode": 0, "access_token": "synthetic-token", "expires_in": 7200}
            )
        if request.url.path == "/cgi-bin/message/send":
            assert json.loads(request.content)["enable_duplicate_check"] == 1
            return httpx.Response(200, json={"errcode": 0, "msgid": "synthetic-msgid-1"})
        raise AssertionError("unexpected WeCom request")

    robot_ref = ConnectionRef("wecom", "wecom-robot")
    application_ref = ConnectionRef("wecom", "wecom-application")
    credentials = StaticCredentialProvider(
        {
            robot_ref: Credentials(
                auth_method_id="group-robot", webhook_key=SecretString("SYNTHETIC-ROBOT-KEY")
            ),
            application_ref: Credentials(
                auth_method_id="application",
                corp_id="ww1a2b3c4d5e6f7a8b",
                agent_id="1000002",
                corp_secret=SecretString("SYNTHETIC_CORP_SECRET"),
            ),
        }
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), credentials, http_client)
        robot_connection = new_connection(provider, robot_ref)
        application_connection = new_connection(provider, application_ref)
        robot_flow = WeComGroupRobotFlow(robot_connection)
        application_flow = WeComApplicationFlow(application_connection)
        registry = dex.Registry([robot_flow, application_flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                robot_id = f"wecom-robot-python-{uuid4().hex}"
                await client.start_flow(
                    robot_flow,
                    robot_id,
                    SendGroupRobotMessageInput(message_type="text", content="Deployment complete"),
                )
                robot_result = await asyncio.wait_for(
                    client.wait_for_flow(robot_id, timedelta(seconds=25)), 30
                )
                assert robot_result.status == dex.FlowStatus.COMPLETED, robot_result.error_message
                assert robot_result.single_output(SentGroupRobotMessage).content_bytes == 19
                assert robot_sends == 2

                application_id = f"wecom-application-python-{uuid4().hex}"
                await client.start_flow(
                    application_flow,
                    application_id,
                    SendApplicationMessageInput(
                        message_type="text", content="Incident open", to_user_ids=("alice",)
                    ),
                )
                application_result = await asyncio.wait_for(
                    client.wait_for_flow(application_id, timedelta(seconds=25)), 30
                )
                assert application_result.status == dex.FlowStatus.COMPLETED, application_result.error_message
                assert (
                    application_result.single_output(SentApplicationMessage).message_id == "synthetic-msgid-1"
                )
                assert len(requests) == 4
        finally:
            await worker.stop()
            await worker_task
            cache.close()
