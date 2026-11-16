# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the button approval example on a real Dex Worker and Client with Socket Mode and Slack stand-ins."""

import asyncio
import os
import tempfile
from collections.abc import Callable
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError

from dex_connectors.slack import SlackTriggerOptions, SlackTriggerTimings, new_project_connection
from tests.slack_trigger_fakes import (
    FakeDialer,
    FakeSocket,
    RecordingLogger,
    SlackWebAPI,
    binding,
    click_envelope,
    events_envelope,
    root_message,
    socket_mode_project,
)

from .flow import (
    APPROVAL_REQUEST_TEXT,
    BUTTON_TRIGGER_BINDING,
    CONNECTION_NAME,
    START_TRIGGER_BINDING,
    ApprovalState,
    ButtonApprovalFlow,
)
from .receiver import open_button_approval_runner

TIMINGS = SlackTriggerTimings(timedelta(milliseconds=5), timedelta(milliseconds=20), timedelta(seconds=60))


async def eventually(predicate: Callable[[], bool], timeout: float = 30) -> None:
    deadline = asyncio.get_running_loop().time() + timeout
    while not predicate():
        if asyncio.get_running_loop().time() > deadline:
            raise AssertionError("condition was not reached")
        await asyncio.sleep(0.05)


async def wait_for_status(
    client: dex.AsyncClient, flow: ButtonApprovalFlow, flow_id: str, status: str
) -> None:
    for _ in range(600):
        try:
            state = await client.invoke_rpc(flow.get_approval_status, flow_id)
            if state.status == status:
                return
        except (dex.FlowNotFoundError, dex.FlowNotActiveOrNotFoundError, dex.RpcLockConflictError):
            pass  # The Flow has not started, or a Step holds the state while it runs.
        await asyncio.sleep(0.05)
    raise AssertionError(f"Flow {flow_id} did not reach {status}")


@pytest.mark.asyncio
async def test_first_decider_click_on_the_posted_buttons_decides_once() -> None:
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    channel_id = "C" + uuid4().hex[:10].upper()
    project = await socket_mode_project(
        CONNECTION_NAME,
        [
            binding(
                "channelThreadCreated",
                CONNECTION_NAME,
                START_TRIGGER_BINDING,
                {"channelId": channel_id, "threadTriggerMatcher": {"messageContains": "request approval"}},
            ),
            binding(
                "buttonClicked",
                CONNECTION_NAME,
                BUTTON_TRIGGER_BINDING,
                {"channelId": channel_id, "clickerUserIds": ["U2"]},
            ),
        ],
    )
    web_api, socket, logger = SlackWebAPI(channel_id), FakeSocket(), RecordingLogger()
    dialer = FakeDialer(socket)
    connection = new_project_connection(
        project, CONNECTION_NAME, httpx.AsyncClient(transport=httpx.MockTransport(web_api.answer))
    )
    flow = ButtonApprovalFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    flow_id = f"slack-button-approval-T1-{channel_id}-1.0"
    with tempfile.TemporaryDirectory(prefix="slack-python-button-approval-") as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                runner = await open_button_approval_runner(
                    project,
                    client,
                    flow,
                    httpx.AsyncClient(transport=httpx.MockTransport(web_api.answer)),
                    SlackTriggerOptions(logger=logger, socket_dialer=dialer, timings=TIMINGS),
                )
                context = TriggerContext()
                running = asyncio.create_task(runner.run(context))
                try:
                    socket.push(
                        events_envelope("e1", f"EvRoot-{channel_id}", root_message("1.0", channel_id))
                    )
                    await wait_for_status(client, flow, flow_id, "waitingForDecision")
                    buttons = web_api.posts[0]
                    assert (buttons["thread_ts"], buttons["text"]) == ("1.0", APPROVAL_REQUEST_TEXT)
                    assert [element["action_id"] for element in buttons["blocks"][1]["elements"]] == [
                        "approve",
                        "reject",
                    ]
                    assert {element["value"] for element in buttons["blocks"][1]["elements"]} == {"1.0"}
                    socket.push(
                        click_envelope("e2", channel_id, "1.9", "approve", "1.0", user="U7", action_ts="3.1")
                    )
                    socket.push(click_envelope("e3", channel_id, "5.5", "approve", "1.0", action_ts="3.2"))
                    socket.push(click_envelope("e4", channel_id, "1.9", "reject", "1.0", action_ts="3.3"))
                    result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=30)), 40)
                    assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                    state = result.single_output(ApprovalState)
                    assert (state.decision, state.decider_user_id) == ("rejected", "U2")
                    assert state.decision_event_id == f"T1:{channel_id}:1.9:reject:3.3"
                    socket.push(click_envelope("e5", channel_id, "1.9", "approve", "1.0", action_ts="3.4"))
                    late = f"T1:{channel_id}:1.9:approve:3.4"
                    await eventually(
                        lambda: bool(logger.find("trigger event skipped: undeliverable", event_id=late))
                    )
                    assert socket.acknowledged == ["e1", "e2", "e3", "e4", "e5"]
                    assert logger.find("trigger event ignored", reason="clicker_mismatch", envelope_id="e2")
                finally:
                    context.cancellation.cancel()
                    with pytest.raises(TriggerDeliveryAbortedError):
                        await running
                    await runner.aclose()
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
    assert [post["text"] for post in web_api.posts] == [APPROVAL_REQUEST_TEXT, "Rejected by <@U2>."]
    assert dialer.dialed and "SENTINEL" not in repr(logger.records)
