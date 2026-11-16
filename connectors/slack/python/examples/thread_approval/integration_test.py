# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the thread approval example on a real Dex Worker and Client in Socket Mode and in relayed mode."""

import asyncio
import os
import tempfile
from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.slack import (
    MessageTriggerRunner,
    SlackTriggerOptions,
    SlackTriggerTimings,
    new_project_connection,
)
from tests.slack_trigger_fakes import (
    FakeDialer,
    FakeSocket,
    RecordingLogger,
    RelayHost,
    SlackWebAPI,
    binding,
    events_envelope,
    events_payload,
    relayed_request,
    reply_message,
    root_message,
    socket_mode_project,
)

from .flow import (
    COMPLETION_TEXT,
    CONNECTION_NAME,
    REPLY_TRIGGER_BINDING,
    START_TRIGGER_BINDING,
    ThreadApprovalFlow,
    ThreadState,
)
from .receiver import RelayedTriggerRoute, open_thread_approval_runner

TIMINGS = SlackTriggerTimings(timedelta(milliseconds=5), timedelta(milliseconds=20), timedelta(seconds=60))


def bindings(channel_id: str) -> list[dict[str, object]]:
    return [
        binding(
            "channelThreadCreated",
            CONNECTION_NAME,
            START_TRIGGER_BINDING,
            {"channelId": channel_id, "threadTriggerMatcher": {"messageContains": "request"}},
        ),
        binding(
            "threadReplyCreated",
            CONNECTION_NAME,
            REPLY_TRIGGER_BINDING,
            {
                "channelId": channel_id,
                "threadReplyMatcher": {"messageContains": "approved", "posterUserIds": ["U2"]},
            },
        ),
    ]


@dataclass
class Application:
    client: dex.AsyncClient
    flow: ThreadApprovalFlow
    runner: MessageTriggerRunner
    logger: RecordingLogger


@asynccontextmanager
async def application(
    project: LoadedProject, web_api: SlackWebAPI, dialer: FakeDialer | None = None
) -> AsyncIterator[Application]:
    """Run the example's Worker and runner against the Dex server until the block ends."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    logger = RecordingLogger()
    http_client = httpx.AsyncClient(transport=httpx.MockTransport(web_api.answer))
    connection = new_project_connection(project, CONNECTION_NAME, http_client)
    flow = ThreadApprovalFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    with tempfile.TemporaryDirectory(prefix="slack-python-thread-approval-") as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                options = SlackTriggerOptions(logger=logger, socket_dialer=dialer, timings=TIMINGS)
                runner_http_client = httpx.AsyncClient(transport=httpx.MockTransport(web_api.answer))
                runner = await open_thread_approval_runner(project, client, flow, runner_http_client, options)
                context = TriggerContext()
                running = asyncio.create_task(runner.run(context))
                try:
                    yield Application(client, flow, runner, logger)
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


async def eventually(predicate: Callable[[], bool], timeout: float = 30) -> None:
    deadline = asyncio.get_running_loop().time() + timeout
    while not predicate():
        if asyncio.get_running_loop().time() > deadline:
            raise AssertionError("condition was not reached")
        await asyncio.sleep(0.05)


async def wait_for_completion(client: dex.AsyncClient, flow_id: str) -> ThreadState:
    for _ in range(600):
        try:
            result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=30)), 40)
        except dex.FlowNotFoundError:
            await asyncio.sleep(0.05)
            continue
        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
        return result.single_output(ThreadState)
    raise AssertionError(f"Flow {flow_id} was not started")


async def wait_for_status(app: Application, flow_id: str, status: str) -> None:
    for _ in range(600):
        try:
            state = await app.client.invoke_rpc(app.flow.get_thread_status, flow_id)
            if state.status == status:
                return
        except (dex.FlowNotFoundError, dex.FlowNotActiveOrNotFoundError, dex.RpcLockConflictError):
            pass  # The Flow has not started, or a Step holds the state while it runs.
        await asyncio.sleep(0.05)
    raise AssertionError(f"Flow {flow_id} did not reach {status}")


def unique_channel() -> str:
    return "C" + uuid4().hex[:10].upper()


@pytest.mark.asyncio
async def test_socket_mode_root_starts_one_flow_a_reply_approves_it_and_an_orphan_is_consumed() -> None:
    channel_id = unique_channel()
    project = await socket_mode_project(CONNECTION_NAME, bindings(channel_id))
    web_api, socket = SlackWebAPI(channel_id), FakeSocket()
    async with application(project, web_api, FakeDialer(socket)) as app:
        socket.push(events_envelope("e1", f"EvRoot-{channel_id}", root_message("1.0", channel_id)))
        socket.push(events_envelope("e2", f"EvRoot-{channel_id}", root_message("1.0", channel_id)))
        reply = reply_message("1.1", "1.0", channel_id)
        socket.push(events_envelope("e3", f"EvReply-{channel_id}", reply))
        socket.push(events_envelope("e4", f"EvReply-{channel_id}", reply))
        socket.push(events_envelope("e5", f"EvOrphan-{channel_id}", reply_message("7.1", "7.0", channel_id)))
        completed = await wait_for_completion(app.client, f"slack-thread-approval-T1-{channel_id}-1.0")
        assert completed.reply_event_id == f"EvReply-{channel_id}" and completed.reply_user_id == "U2"
        assert completed.messages[0].text == "please request approval"
        await eventually(
            lambda: (
                len(
                    app.logger.find("trigger event skipped: undeliverable", event_id=f"EvOrphan-{channel_id}")
                )
                == 1
            )
        )
        assert socket.acknowledged == ["e1", "e2", "e3", "e4", "e5"]
    assert [(post["channel"], post["thread_ts"], post["text"]) for post in web_api.posts] == [
        (channel_id, "1.0", COMPLETION_TEXT)
    ]
    assert "SENTINEL" not in repr(app.logger.records)


@pytest.mark.asyncio
async def test_relayed_root_starts_the_flow_and_a_relayed_reply_approves_it() -> None:
    channel_id = unique_channel()
    host = RelayHost(CONNECTION_NAME, bindings(channel_id))
    web_api = SlackWebAPI(channel_id)
    async with application(host.project, web_api) as app:
        await eventually(lambda: len(host.registrations) == 1)
        route = RelayedTriggerRoute(app.runner)
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=route), base_url="http://app"
        ) as relay:
            path = "/triggers/slack/" + "c2xhY2std29ya3NwYWNl"
            root = events_payload(f"EvRoot-{channel_id}", root_message("1.0", channel_id))
            forged_headers, forged_body = relayed_request(root, "forged-token")
            assert (await relay.post(path, content=forged_body, headers=forged_headers)).status_code == 400
            for _ in range(2):
                headers, body = relayed_request(root)
                assert (await relay.post(path, content=body, headers=headers)).status_code == 200
            flow_id = f"slack-thread-approval-T1-{channel_id}-1.0"
            await wait_for_status(app, flow_id, "waitingForReply")
            reply = events_payload(f"EvReply-{channel_id}", reply_message("1.1", "1.0", channel_id))
            headers, body = relayed_request(reply)
            assert (await relay.post(path, content=body, headers=headers)).status_code == 200
            assert (await relay.post("/elsewhere", content=body, headers=headers)).status_code == 404
            completed = await wait_for_completion(app.client, flow_id)
        assert completed.reply_event_id == f"EvReply-{channel_id}"
        await eventually(lambda: len(web_api.posts) == 1)
    pending = await host.pending_event_ids(
        ("channelThreadCreated", START_TRIGGER_BINDING), ("threadReplyCreated", REPLY_TRIGGER_BINDING)
    )
    assert pending == []
    assert host.credential_reads >= 1
    assert "SENTINEL" not in repr(app.logger.records)
