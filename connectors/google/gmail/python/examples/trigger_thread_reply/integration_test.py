# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Gmail thread reply application against a real Dex server and a fake Gmail API."""

import asyncio
import os
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass
from datetime import timedelta
from typing import Any, cast
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import TriggerCancellation
from dex_connectors.sdk.projectconfig import (
    Configuration,
    InMemoryObjectStore,
    LoadedProject,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
)

from tests.gmail_inbox_fake import (
    FakeGmail,
    RecordingLogger,
    fake_connection,
    fake_http_client,
    reply_message,
    root_message,
)

from .app import run_thread_reply_application
from .flow import REPLY_TEXT, REPLY_TRIGGER_BINDING, START_TRIGGER_BINDING, GmailThreadReplyFlow, ThreadState

SCOPE = Scope("gmail-trigger-thread-reply", "live")


class SharedProject:
    """Project storage that both application processes of one test share."""

    def __init__(self, start_configuration: dict[str, Any], reply_configuration: dict[str, Any]) -> None:
        self.objects = InMemoryObjectStore()
        bindings = (
            ("messageReceived", START_TRIGGER_BINDING, start_configuration),
            ("replyReceived", REPLY_TRIGGER_BINDING, reply_configuration),
        )
        self.configuration = Configuration(
            SCOPE,
            1,
            (),
            tuple(
                {
                    "connectorId": "gmail",
                    "connectionName": "gmail-inbox",
                    "triggerName": trigger,
                    "bindingName": binding,
                    "configuration": configuration,
                }
                for trigger, binding, configuration in bindings
            ),
        )

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)


async def eventually(condition: Callable[[], Awaitable[bool] | bool], timeout: float = 30) -> None:
    """Poll a sync or async condition until it holds."""
    deadline = time.monotonic() + timeout
    while True:
        result = condition()
        if not isinstance(result, bool):
            result = await result
        if result:
            return
        assert time.monotonic() < deadline, "condition did not converge"
        await asyncio.sleep(0.05)


@dataclass
class RunningApplication:
    dex_client: dex.AsyncClient
    flow: GmailThreadReplyFlow
    logger: RecordingLogger


@asynccontextmanager
async def running_application(project: SharedProject, gmail: FakeGmail) -> AsyncIterator[RunningApplication]:
    """Run one application process until the block exits."""
    logger = RecordingLogger()
    cancellation = TriggerCancellation()
    ready: asyncio.Future[tuple[dex.AsyncClient, GmailThreadReplyFlow]] = (
        asyncio.get_running_loop().create_future()
    )
    async with fake_http_client(gmail) as http_client:
        connection = fake_connection(gmail, http_client, logger)
        task = asyncio.create_task(
            run_thread_reply_application(
                cast(LoadedProject, project),
                connection,
                os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801"),
                cancellation,
                logger=logger,
                on_ready=lambda client, flow: ready.set_result((client, flow)),
            )
        )
        try:
            started: set[asyncio.Future[Any]] = {task, ready}
            done, _ = await asyncio.wait(started, timeout=60, return_when=asyncio.FIRST_COMPLETED)
            if task in done:
                task.result()
            dex_client, flow = await ready
            yield RunningApplication(dex_client, flow, logger)
        finally:
            cancellation.cancel()
            await task


async def thread_status(application: RunningApplication, flow_id: str) -> str:
    try:
        state = await application.dex_client.invoke_rpc(application.flow.get_thread_status, flow_id)
    except Exception:  # noqa: BLE001
        # The Flow may not have started yet.
        return "absent"
    return state.status


async def wait_for_completed_state(application: RunningApplication, flow_id: str) -> ThreadState:
    result = await asyncio.wait_for(application.dex_client.wait_for_flow(flow_id, timedelta(seconds=60)), 70)
    assert result.status == dex.FlowStatus.COMPLETED, result.error_message
    return result.single_output(ThreadState)


@pytest.mark.asyncio
async def test_starts_one_flow_per_root_replies_after_the_matching_reply_and_consumes_an_orphan() -> None:
    suffix = uuid4().hex[:12]
    thread = f"thread-{suffix}"
    flow_id = f"gmail-thread-reply-gmail-inbox-{thread}"
    project = SharedProject(
        {"messageMatcher": {"messageContains": "order", "senderEmails": ["ada@example.com"]}},
        {"replyMatcher": {"messageContains": "approved"}},
    )
    gmail = FakeGmail()
    # A root from a sender outside the allowlist and a reply whose Flow never started arrive first.
    gmail.receive(
        root_message(f"ignored-{suffix}", f"ignored-thread-{suffix}", sender="Eve <eve@example.com>")
    )
    gmail.receive(reply_message(f"orphan-{suffix}", f"orphan-thread-{suffix}"))
    gmail.receive(root_message(f"root-{suffix}", thread))
    async with running_application(project, gmail) as application:

        async def is_waiting() -> bool:
            return await thread_status(application, flow_id) == "waitingForReply"

        await eventually(is_waiting)
        gmail.receive(reply_message(f"reply-{suffix}", thread, snippet="Not yet"))
        gmail.receive(reply_message(f"approved-{suffix}", thread))
        state = await wait_for_completed_state(application, flow_id)
        assert state.reply_event_id == f"approved-{suffix}"
        assert state.reply_message_id == f"approved-{suffix}"
        assert state.root_message is not None and state.root_message.message_id == f"root-{suffix}"
        assert len(gmail.sent) == 1
        assert f"In-Reply-To: <approved-{suffix}@mail.example.com>" in gmail.sent[0]
        assert REPLY_TEXT in gmail.sent[0]
        logger = application.logger
        await eventually(
            lambda: bool(logger.find("trigger event skipped: undeliverable", event_id=f"orphan-{suffix}"))
        )
        assert logger.find("trigger event ignored", event_id=f"ignored-{suffix}", reason="matcher_mismatch")
        assert logger.find("trigger event ignored", event_id=f"reply-{suffix}", reason="matcher_mismatch")
        recorded = repr([record.attributes for record in logger.records])
        assert (
            "SENTINEL" not in recorded and "ada@example" not in recorded and "Order question" not in recorded
        )


@pytest.mark.asyncio
async def test_retries_a_reply_while_its_flow_reads_the_root_and_absorbs_a_restart_rescan() -> None:
    suffix = uuid4().hex[:12]
    thread = f"thread-{suffix}"
    flow_id = f"gmail-thread-reply-gmail-inbox-{thread}"
    project = SharedProject({}, {})
    gmail = FakeGmail()
    # The Flow's root read waits, so the reply of the same poll reaches a Flow still reading it.
    release_root_read = asyncio.Event()
    gmail.full_read_gates[f"root-{suffix}"] = release_root_read
    gmail.receive(root_message(f"root-{suffix}", thread))
    gmail.receive(reply_message(f"reply-{suffix}", thread))
    try:
        async with running_application(project, gmail) as first:
            await eventually(
                lambda: bool(
                    first.logger.find(
                        "trigger delivery failed; retrying", event_id=f"reply-{suffix}", attempt=1
                    )
                )
            )
            release_root_read.set()
            state = await wait_for_completed_state(first, flow_id)
            assert state.reply_event_id == f"reply-{suffix}"
            await eventually(
                lambda: bool(first.logger.find("trigger delivered after retry", event_id=f"reply-{suffix}"))
            )
    finally:
        release_root_read.set()
    # The restarted process rescans the page: the Flow start is a duplicate and the reply finds a closed Flow.
    async with running_application(project, gmail) as second:
        await eventually(
            lambda: bool(
                second.logger.find("trigger event delivered", event_id=f"root-{suffix}", duplicate=True)
            )
        )
        await eventually(
            lambda: bool(
                second.logger.find("trigger event skipped: undeliverable", event_id=f"reply-{suffix}")
            )
        )
    assert len(gmail.sent) == 1
    assert REPLY_TEXT in gmail.sent[0]
