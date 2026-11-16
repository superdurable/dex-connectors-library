# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Socket Mode acknowledgement while deliveries wait, as the Go connector's queue tests require."""

from __future__ import annotations

import asyncio
import dataclasses
from typing import Any

import httpx
from dex_connectors.sdk import TriggerContext, TriggerEvent
from slack_trigger_fakes import (
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
from test_triggers import (
    CHANNEL,
    CLICK,
    CONNECTION,
    START,
    RecordingTarget,
    credentials,
    eventually,
    options,
    running,
    static_connection,
)

import dex_connectors.slack as slack
from dex_connectors.slack._socket_mode_runner import until_cancelled


class GatedTarget(RecordingTarget):
    """Records each event, then holds its delivery until the test opens the gate."""

    def __init__(self, name: str, delivered: list[tuple[str, str]]) -> None:
        super().__init__(name, delivered)
        self.gate = asyncio.Event()
        self.started: list[str] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        self.started.append(event.id)
        # Cancellation ends the wait, so a failed assertion cannot leave the runner stuck.
        await until_cancelled(self.gate.wait(), context.cancellation)
        await super().handle_trigger(context, event)


async def test_click_is_acknowledged_while_an_earlier_root_waits_and_is_delivered_after_it() -> None:
    project = await socket_mode_project(CONNECTION, [START, CLICK])
    socket, logger, delivered = FakeSocket(), RecordingLogger(), []
    roots, clicks = GatedTarget("root", delivered), RecordingTarget("click", delivered)
    runner = await slack.new_project_message_trigger_runner(
        project,
        CONNECTION,
        channel_thread_created_routes=[slack.ProjectChannelThreadCreatedTriggerRoute("start", roots)],
        button_clicked_routes=[slack.ProjectButtonClickedTriggerRoute("decision", clicks)],
        http_client=httpx.AsyncClient(transport=httpx.MockTransport(SlackWebAPI(CHANNEL).answer)),
        options=options(FakeDialer(socket), logger),
    )
    async with running(runner):
        socket.push(events_envelope("e1", "EvRoot", root_message("1.0", CHANNEL)))
        socket.push(click_envelope("e2", CHANNEL, "1.9", "approve", "1.0"))
        await eventually(lambda: socket.acknowledged == ["e1", "e2"] and roots.started == ["EvRoot"])
        assert delivered == []
        roots.gate.set()
        await eventually(lambda: len(delivered) == 2)
    assert delivered == [("root", "EvRoot"), ("click", "T1:C1:1.9:approve:1760000001.000100")]


async def test_a_full_delivery_queue_stops_acknowledging_until_delivery_catches_up() -> None:
    # The flood has its own binding, because a durable inbox holds its lock during a delivery attempt.
    flood = binding(
        "channelThreadCreated",
        CONNECTION,
        "flood",
        {"channelId": CHANNEL, "threadTriggerMatcher": {"messageContains": "flood"}},
    )
    project = await socket_mode_project(CONNECTION, [START, flood])
    socket, logger, delivered = FakeSocket(), RecordingLogger(), []
    stuck, floods = GatedTarget("stuck", delivered), RecordingTarget("flood", delivered)
    runner = await slack.new_project_message_trigger_runner(
        project,
        CONNECTION,
        channel_thread_created_routes=[
            slack.ProjectChannelThreadCreatedTriggerRoute("start", stuck),
            slack.ProjectChannelThreadCreatedTriggerRoute("flood", floods),
        ],
        http_client=httpx.AsyncClient(transport=httpx.MockTransport(SlackWebAPI(CHANNEL).answer)),
        options=dataclasses.replace(options(FakeDialer(socket), logger), delivery_queue_capacity=1),
    )
    async with running(runner):
        socket.push(events_envelope("e0", "EvStuck", root_message("1.0", CHANNEL)))
        await eventually(lambda: stuck.started == ["EvStuck"])
        for index in range(1, 6):
            message = root_message(f"{index + 1}.0", CHANNEL, text="flood")
            socket.push(events_envelope(f"e{index}", f"EvFlood{index}", message))
        # One queued envelope, plus the one the reader holds while it waits for room.
        await eventually(lambda: len(socket.acknowledged) == 3)
        # Waiting proves the absence of a fourth acknowledgement while delivery is stuck.
        await asyncio.sleep(0.05)
        assert socket.acknowledged == ["e0", "e1", "e2"] and delivered == []
        stuck.gate.set()
        await eventually(lambda: len(delivered) == 6)
    assert [event_id for _, event_id in delivered] == ["EvStuck"] + [
        f"EvFlood{index}" for index in range(1, 6)
    ]
    assert socket.acknowledged == ["e0", "e1", "e2", "e3", "e4", "e5"]


async def test_without_durable_inboxes_each_event_is_delivered_before_the_next_is_read() -> None:
    socket, logger, delivered = FakeSocket(), RecordingLogger(), []
    roots = GatedTarget("root", delivered)
    connection = static_connection(credentials("xapp" + "-SENTINEL-APP"), SlackWebAPI(CHANNEL))
    route = slack.ChannelThreadCreatedTriggerRoute(
        "start", slack.ChannelThreadCreatedTriggerConfiguration(CHANNEL), roots
    )
    runner = slack.new_message_trigger_runner(
        connection, channel_thread_created_routes=[route], options=options(FakeDialer(socket), logger)
    )
    async with running(runner):
        socket.push(events_envelope("e1", "Ev1", root_message("1.0", CHANNEL)))
        socket.push(events_envelope("e2", "Ev2", root_message("2.0", CHANNEL)))
        await eventually(lambda: roots.started == ["Ev1"])
        # Waiting proves the second envelope stays unread while the first is delivered.
        await asyncio.sleep(0.05)
        assert socket.acknowledged == ["e1"]
        roots.gate.set()
        await eventually(lambda: len(delivered) == 2)
    assert socket.acknowledged == ["e1", "e2"]
