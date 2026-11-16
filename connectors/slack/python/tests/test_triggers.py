# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Socket Mode and relayed delivery of the Slack message and button click Triggers."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.projectconfig import LoadedProject, TriggerInboxKey, encode_trigger_event
from slack_trigger_fakes import (
    APP_TOKEN,
    BOT_TOKEN,
    SOCKET_URL,
    FakeDialer,
    FakeSocket,
    RecordingLogger,
    RelayHost,
    SlackWebAPI,
    binding,
    click_envelope,
    events_envelope,
    events_payload,
    relayed_request,
    reply_message,
    root_message,
    socket_mode_project,
)

import dex_connectors.slack as slack

CONNECTION = "workspace"
CHANNEL = "C1"
TIMINGS = slack.SlackTriggerTimings(
    timedelta(milliseconds=5), timedelta(milliseconds=20), timedelta(seconds=60)
)
START = binding(
    "channelThreadCreated",
    CONNECTION,
    "start",
    {"channelId": CHANNEL, "threadTriggerMatcher": {"messageContains": "request"}},
)
REPLY = binding(
    "threadReplyCreated",
    CONNECTION,
    "reply",
    {"channelId": CHANNEL, "threadReplyMatcher": {"posterUserIds": ["U2"]}},
)
CLICK = binding("buttonClicked", CONNECTION, "decision", {"channelId": CHANNEL, "clickerUserIds": ["U2"]})


class RecordingTarget:
    """Records delivered events in order; the shared list shows the order across targets."""

    def __init__(self, name: str, delivered: list[tuple[str, str]]) -> None:
        self.name = name
        self.delivered = delivered
        self.events: list[TriggerEvent[Any]] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        self.events.append(event)
        self.delivered.append((self.name, event.id))


async def eventually(predicate: Callable[[], bool], timeout: float = 5) -> None:
    """Poll until predicate holds, failing after timeout seconds."""
    deadline = asyncio.get_running_loop().time() + timeout
    while not predicate():
        if asyncio.get_running_loop().time() > deadline:
            raise AssertionError("condition was not reached")
        await asyncio.sleep(0.005)


@asynccontextmanager
async def running(runner: slack.MessageTriggerRunner) -> AsyncIterator[asyncio.Task[None]]:
    """Run the runner until the block ends, then require that it stopped by cancellation."""
    context = TriggerContext()
    task = asyncio.create_task(runner.run(context))
    try:
        yield task
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await runner.aclose()


def options(dialer: FakeDialer, logger: RecordingLogger) -> slack.SlackTriggerOptions:
    return slack.SlackTriggerOptions(logger=logger, socket_dialer=dialer, timings=TIMINGS)


async def project_runner(
    project: LoadedProject,
    web_api: SlackWebAPI,
    dialer: FakeDialer,
    logger: RecordingLogger,
    delivered: list[tuple[str, str]],
    *,
    with_reply: bool = True,
    with_click: bool = False,
) -> tuple[slack.MessageTriggerRunner, RecordingTarget, RecordingTarget, RecordingTarget]:
    roots, replies, clicks = (RecordingTarget(name, delivered) for name in ("root", "reply", "click"))
    runner = await slack.new_project_message_trigger_runner(
        project,
        CONNECTION,
        channel_thread_created_routes=[slack.ProjectChannelThreadCreatedTriggerRoute("start", roots)],
        thread_reply_created_routes=[slack.ProjectThreadReplyCreatedTriggerRoute("reply", replies)]
        if with_reply
        else (),
        button_clicked_routes=[slack.ProjectButtonClickedTriggerRoute("decision", clicks)]
        if with_click
        else (),
        http_client=httpx.AsyncClient(transport=httpx.MockTransport(web_api.answer)),
        options=options(dialer, logger),
    )
    return runner, roots, replies, clicks


async def test_socket_mode_records_acknowledges_and_delivers_roots_before_replies() -> None:
    project = await socket_mode_project(CONNECTION, [START, REPLY])
    web_api, socket, logger, delivered = SlackWebAPI(CHANNEL), FakeSocket(), RecordingLogger(), []
    dialer = FakeDialer(socket)
    runner, roots, replies, _ = await project_runner(project, web_api, dialer, logger, delivered)
    async with running(runner):
        socket.push(events_envelope("e1", "EvRoot", root_message("1.0", CHANNEL)))
        socket.push(events_envelope("e2", "EvBot", {**root_message("2.0", CHANNEL), "bot_id": "B1"}))
        socket.push(
            events_envelope("e3", "EvEdit", {**root_message("3.0", CHANNEL), "subtype": "message_changed"})
        )
        socket.push(events_envelope("e4", "EvOther", root_message("4.0", "C9")))
        socket.push(events_envelope("e5", "EvReply", reply_message("1.1", "1.0", CHANNEL)))
        socket.push(events_envelope("e6", "EvStranger", reply_message("1.2", "1.0", CHANNEL, user="U7")))
        socket.push({"envelope_id": "e7", "type": "events_api", "payload": {"event_time": "soon"}})
        socket.push({"type": "hello"})
        await eventually(lambda: len(socket.acknowledged) == 7 and len(delivered) == 2)
    assert delivered == [("root", "EvRoot"), ("reply", "EvReply")]
    assert socket.acknowledged == ["e1", "e2", "e3", "e4", "e5", "e6", "e7"]
    assert roots.events[0].payload == slack.MessageEvent(
        "T1", CHANNEL, "1.0", "1.0", "U1", "please request approval"
    )
    assert roots.events[0].occurred_at == datetime.fromtimestamp(1_760_000_000, UTC)
    assert replies.events[0].payload.thread_timestamp == "1.0"
    assert dialer.dialed == [SOCKET_URL]
    assert logger.find("trigger event ignored", reason="bot", binding="start")
    assert logger.find("trigger event ignored", reason="subtype", subtype="message_changed")
    assert logger.find("trigger event ignored", reason="not_a_reply", binding="reply")
    assert logger.find("trigger event ignored", reason="matcher_mismatch", binding="reply")
    assert logger.find("trigger event skipped: undecodable", envelope_id="e7")
    for inbox in ("start", "reply"):
        trigger = "channelThreadCreated" if inbox == "start" else "threadReplyCreated"
        assert (
            await project.trigger_inbox(TriggerInboxKey("slack", CONNECTION, trigger, inbox)).pending() == ()
        )
    assert "SENTINEL" not in repr(logger.records)


async def test_disconnect_reconnects_without_backoff_and_failures_back_off() -> None:
    project = await socket_mode_project(CONNECTION, [START])
    first, second, third = FakeSocket(), FakeSocket(), FakeSocket()
    web_api, logger, delivered = SlackWebAPI(CHANNEL), RecordingLogger(), []
    dialer = FakeDialer(first, second, third)
    runner, _, _, _ = await project_runner(project, web_api, dialer, logger, delivered, with_reply=False)
    async with running(runner):
        first.push({"type": "disconnect", "reason": "refresh_requested"})
        second.fail()
        third.push(events_envelope("e1", "EvRoot", root_message("1.0", CHANNEL)))
        await eventually(lambda: delivered == [("root", "EvRoot")])
    assert first.is_closed and second.is_closed
    assert logger.find("slack socket mode disconnected; reconnecting", reason="refresh_requested", delay_ms=5)
    (failed,) = logger.find("slack socket mode connection failed; reconnecting")
    assert failed["attempt"] == 1 and "SENTINEL" not in str(failed["error"])
    assert [record["reconnect"] for record in logger.find("slack socket mode connected")] == [
        False,
        True,
        True,
    ]


async def test_button_clicks_share_the_socket_and_are_recorded_before_acknowledgement() -> None:
    project = await socket_mode_project(CONNECTION, [START, CLICK])
    web_api, socket, logger, delivered = SlackWebAPI(CHANNEL), FakeSocket(), RecordingLogger(), []
    runner, _, _, clicks = await project_runner(
        project, web_api, FakeDialer(socket), logger, delivered, with_reply=False, with_click=True
    )
    async with running(runner):
        socket.push(events_envelope("e1", "EvRoot", root_message("1.0", CHANNEL)))
        socket.push(click_envelope("e2", CHANNEL, "1.9", "approve", "1.0"))
        socket.push(click_envelope("e3", CHANNEL, "1.9", "approve", "1.0", user="U7"))
        socket.push(click_envelope("e4", "C9", "1.9", "approve", "1.0"))
        await eventually(lambda: len(socket.acknowledged) == 4 and len(delivered) == 2)
    assert delivered == [("root", "EvRoot"), ("click", "T1:C1:1.9:approve:1760000001.000100")]
    assert clicks.events[0].payload == slack.ButtonClickEvent(
        "T1", CHANNEL, "1.9", "1.0", "U2", "approve", "1.0", "1760000001.000100"
    )
    assert clicks.events[0].occurred_at == datetime(2025, 10, 9, 8, 53, 21, 100, tzinfo=UTC)
    assert logger.find("trigger event ignored", reason="clicker_mismatch", envelope_id="e3")
    assert logger.find("trigger event ignored", reason="channel_mismatch", envelope_id="e4")


def test_block_actions_decode_nulls_as_zero_values_and_match_by_action_and_clicker() -> None:
    configuration = slack.ButtonClickedTriggerConfiguration(CHANNEL, ("approve",), ())
    payload = slack.button_trigger.decode_block_actions_payload(
        {
            "type": "block_actions",
            "team": {"id": "T1"},
            "user": {"id": "U1"},
            "container": {"type": "message", "channel_id": CHANNEL, "message_ts": "5.0"},
            "message": None,
            "actions": [
                None,
                {"type": "button", "action_id": "reject", "action_ts": "6.0"},
                {"type": "button", "action_id": "approve", "action_ts": "bad"},
            ],
        }
    )
    events, ignored = slack.button_trigger.match_button_click_payload(configuration, payload)
    assert ignored == ["not_a_button", "action_mismatch"]
    (event,) = events
    assert event.payload.thread_timestamp == "5.0" and event.payload.value == ""
    assert event.occurred_at == datetime(1, 1, 1, tzinfo=UTC)
    assert slack.button_trigger.match_button_click_payload(
        configuration, slack.button_trigger.decode_block_actions_payload(None)
    ) == ([], ["not_block_actions"])
    with pytest.raises(ValueError):
        slack.button_trigger.decode_block_actions_payload({"user": {"id": 7}})


def test_inbox_records_use_go_field_names_and_decode_go_records() -> None:
    # The SDK writes Go's map encoding, with sorted keys; Go decodes either order.
    message = TriggerEvent(
        "Ev1",
        datetime.fromtimestamp(1_760_000_000, UTC),
        slack.MessageEvent("T1", "C1", "1.1", "1.0", "U2", "ok"),
    )
    assert encode_trigger_event(message) == (
        b'{"eventId":"Ev1","occurredAt":"2025-10-09T08:53:20Z","payload":{"channelId":"C1","teamId":"T1",'
        b'"text":"ok","threadTimestamp":"1.0","timestamp":"1.1","userId":"U2"}}'
    )
    click = TriggerEvent(
        "T1:C1:1.9:approve:2.5",
        datetime(1970, 1, 1, 0, 0, 2, 500000, tzinfo=UTC),
        slack.ButtonClickEvent("T1", "C1", "1.9", "1.0", "U2", "approve", "", "2.5"),
    )
    assert encode_trigger_event(click) == (
        b'{"eventId":"T1:C1:1.9:approve:2.5","occurredAt":"1970-01-01T00:00:02.5Z","payload":{"actionId":"approve",'
        b'"actionTimestamp":"2.5","channelId":"C1","messageTimestamp":"1.9","teamId":"T1","threadTimestamp":"1.0",'
        b'"userId":"U2"}}'
    )
    payload = (
        '{"teamId":"T1","channelId":"C1","timestamp":"1.1","threadTimestamp":"1.0","userId":"U2","text":"ok"}'
    )
    assert slack.decode_message_event_payload(payload, payload.encode()) == message.payload


def static_connection(credentials: slack.Credentials, web_api: SlackWebAPI) -> slack.Connection:
    reference = ConnectionRef("slack", CONNECTION)
    client = slack.Client(
        slack.Config(endpoint="http://127.0.0.1:9/api"),
        StaticCredentialProvider({reference: credentials}),
        httpx.AsyncClient(transport=httpx.MockTransport(web_api.answer)),
    )
    return slack.new_connection(client, reference)


def credentials(app_token: str) -> slack.Credentials:
    return slack.Credentials(
        "",
        SecretString(""),
        SecretString(BOT_TOKEN),
        SecretString(""),
        SecretString("user"),
        SecretString(""),
        SecretString(app_token),
    )


async def test_runner_without_an_app_token_retries_and_names_only_the_missing_field() -> None:
    logger, delivered = RecordingLogger(), []
    connection = static_connection(credentials(""), SlackWebAPI(CHANNEL))
    route = slack.ChannelThreadCreatedTriggerRoute(
        "start", slack.ChannelThreadCreatedTriggerConfiguration(CHANNEL), RecordingTarget("root", delivered)
    )
    runner = slack.new_message_trigger_runner(
        connection, channel_thread_created_routes=[route], options=options(FakeDialer(), logger)
    )
    async with running(runner):
        await eventually(lambda: len(logger.find("slack socket mode connection failed; reconnecting")) >= 3)
    attempts = logger.find("slack socket mode connection failed; reconnecting")
    assert [record["delay_ms"] for record in attempts[:3]] == [5, 10, 20]
    assert (
        attempts[0]["error"]
        == "Slack Socket Mode credentials are unavailable: credential app_token is required"
    )
    assert (await runner.handle_request("POST", "/", [], b"{}")).status == 404


async def test_routes_validate_configuration_names_and_durable_targets() -> None:
    connection = static_connection(credentials(APP_TOKEN), SlackWebAPI(CHANNEL))
    target = RecordingTarget("any", [])
    reply = slack.ThreadReplyCreatedTriggerConfiguration(CHANNEL)
    with pytest.raises(ValueError, match="requires at least one poster"):
        slack.new_message_trigger_runner(
            connection,
            thread_reply_created_routes=[slack.ThreadReplyCreatedTriggerRoute("reply", reply, target)],
        )
    root = slack.ChannelThreadCreatedTriggerConfiguration(CHANNEL)
    with pytest.raises(ValueError, match="duplicated"):
        slack.new_message_trigger_runner(
            connection,
            channel_thread_created_routes=[
                slack.ChannelThreadCreatedTriggerRoute("start", root, target),
                slack.ChannelThreadCreatedTriggerRoute(" start ", root, target),
            ],
        )
    with pytest.raises(ValueError, match="routes are required"):
        slack.new_message_trigger_runner(connection)
    button = slack.ButtonClickedTriggerConfiguration(CHANNEL)
    with pytest.raises(ValueError, match="durable inboxes"):
        slack.new_message_trigger_runner(
            connection, button_clicked_routes=[slack.ButtonClickedTriggerRoute("decision", button, target)]
        )
    with pytest.raises(ValueError, match="unique"):
        slack.validate_button_clicked_trigger_configuration(
            slack.ButtonClickedTriggerConfiguration(CHANNEL, ("a", "a"))
        )
    standalone = slack.new_button_clicked_trigger(
        connection=connection,
        connection_name=CONNECTION,
        binding_name="decision",
        configuration=button,
        target=target,
    )
    with pytest.raises(ValueError, match="durable inboxes"):
        await standalone.run(TriggerContext())
    with pytest.raises(ValueError, match="must equal"):
        slack.new_channel_thread_created_trigger(
            connection=connection,
            connection_name="other",
            binding_name="start",
            configuration=root,
            target=target,
        )
    with pytest.raises(ValueError, match="threadTriggerMatcher"):
        slack.decode_channel_thread_created_trigger_configuration(
            {"channelId": CHANNEL, "threadTriggerMatcher": 1}
        )


async def test_standalone_message_trigger_delivers_inline_on_its_own_socket() -> None:
    delivered: list[tuple[str, str]] = []
    socket = FakeSocket()
    trigger = slack.new_channel_thread_created_trigger(
        connection=static_connection(credentials(APP_TOKEN), SlackWebAPI(CHANNEL)),
        connection_name=CONNECTION,
        binding_name="start",
        configuration=slack.ChannelThreadCreatedTriggerConfiguration(CHANNEL),
        target=RecordingTarget("root", delivered),
        options=options(FakeDialer(socket), RecordingLogger()),
    )
    context = TriggerContext()
    task = asyncio.create_task(trigger.run(context))
    socket.push(events_envelope("e1", "EvRoot", root_message("1.0", CHANNEL)))
    await eventually(lambda: delivered == [("root", "EvRoot")] and socket.acknowledged == ["e1"])
    context.cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await task


async def relay_runner(
    host: RelayHost, delivered: list[tuple[str, str]], logger: RecordingLogger, *, with_click: bool = False
) -> slack.MessageTriggerRunner:
    runner, _, _, _ = await project_runner(
        host.project, SlackWebAPI(CHANNEL), FakeDialer(), logger, delivered, with_click=with_click
    )
    return runner


async def test_relayed_events_are_verified_recorded_and_delivered_after_registration() -> None:
    host = RelayHost(CONNECTION, [START, REPLY])
    logger, delivered = RecordingLogger(), []
    runner = await relay_runner(host, delivered, logger)
    path = slack.relayed_trigger_path(CONNECTION)
    assert path == "/triggers/slack/d29ya3NwYWNl"
    root = events_payload("EvRoot", root_message("1.0", CHANNEL))
    headers, body = relayed_request(root)
    assert (await runner.handle_request("POST", path, headers, body)).status == 503
    async with running(runner):
        await eventually(lambda: len(host.registrations) == 1)
        assert host.registrations[0]["body"] == {
            "sharedConnectionId": "conn-slack",
            "connectionName": CONNECTION,
        }
        forged_headers, forged_body = relayed_request(root, "forged-token")
        assert (await runner.handle_request("POST", path, forged_headers, forged_body)).status == 400
        assert (await runner.handle_request("POST", path, *relayed_request(root))).status == 200
        reply = events_payload("EvReply", reply_message("1.1", "1.0", CHANNEL))
        assert (await runner.handle_request("POST", path, *relayed_request(reply))).status == 200
        ignored = events_payload("EvOther", root_message("2.0", "C9"))
        assert (await runner.handle_request("POST", path, *relayed_request(ignored))).status == 200
        assert (await runner.handle_request("POST", path, *relayed_request(b"[1"))).status == 400
        assert (await runner.handle_request("GET", path, [], b"")).status == 405
        await eventually(lambda: delivered == [("root", "EvRoot"), ("reply", "EvReply")])
    assert (
        await host.pending_event_ids(("channelThreadCreated", "start"), ("threadReplyCreated", "reply")) == []
    )
    assert host.credential_reads >= 1
    assert logger.find("slack relayed request refused", reason="invalid_signature")
    assert "SENTINEL" not in repr(logger.records)


async def test_relay_registration_retries_while_unavailable_and_stops_when_not_relayed() -> None:
    host = RelayHost(CONNECTION, [START, REPLY])
    host.registration_answers = [(503, {}), (200, {"status": "NOT_RELAYED"})]
    logger = RecordingLogger()
    runner = await relay_runner(host, [], logger)
    with pytest.raises(ValueError, match="host does not relay its events"):
        await runner.run(TriggerContext())
    await runner.aclose()
    assert len(host.registrations) == 2
    assert logger.find("slack relayed trigger registration failed; retrying", attempt=1)


async def test_relayed_connections_refuse_button_routes() -> None:
    host = RelayHost(CONNECTION, [START, REPLY, CLICK])
    runner = await relay_runner(host, [], RecordingLogger(), with_click=True)
    with pytest.raises(ValueError, match="button clicks reach only Socket Mode"):
        await runner.run(TriggerContext())
    await runner.aclose()
    assert host.registrations == []


async def test_open_socket_mode_connection_reports_only_slack_codes() -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        assert request.headers["authorization"] == f"Bearer {APP_TOKEN}"
        return httpx.Response(200, json={"ok": False, "error": "invalid_auth"})

    client = slack.Client(
        slack.Config(endpoint="http://127.0.0.1:9/api"),
        StaticCredentialProvider({}),
        httpx.AsyncClient(transport=httpx.MockTransport(answer)),
    )
    with pytest.raises(ValueError) as rejected:
        await client.open_socket_mode_connection(APP_TOKEN)
    assert str(rejected.value) == "Slack rejected the Socket Mode connection: invalid_auth (HTTP 200)"
    with pytest.raises(ValueError, match="app_token is invalid"):
        await client.open_socket_mode_connection("xapp bad")
    await client.aclose()
