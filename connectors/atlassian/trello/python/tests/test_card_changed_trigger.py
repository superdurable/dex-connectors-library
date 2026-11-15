# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""cardChanged runner wiring, configuration, inbox payloads, and the project runner."""

import asyncio
import json
from collections.abc import Awaitable, Callable, Mapping
from types import SimpleNamespace
from typing import Any

import dex
import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerLogLevel,
    TriggerLogValue,
)
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConnectionConfiguration,
    ConnectionStore,
    InMemoryObjectStore,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
)

from dex_connectors.atlassian_trello import (
    CARD_CHANGED_TRIGGER_DEFINITION,
    CardChangedEvent,
    CardChangedTriggerConfiguration,
    CardChangedTriggerRoute,
    CardTriggerRunnerConfig,
    Client,
    Config,
    Connection,
    Credentials,
    ProjectCardChangedTriggerRoute,
    decode_card_changed_event_payload,
    decode_card_changed_trigger_configuration,
    define_card_changed_trigger_binding,
    new_card_changed_trigger,
    new_card_trigger_runner,
    new_connection,
    new_project_card_trigger_runner,
)

from .trello_board_fake import API_KEY, TOKEN, TrelloBoard

REFERENCE = ConnectionRef("trello", "trello-cards")
SCOPE = Scope("trello-trigger-test", "live")
FIXTURE_EVENT = json.loads(
    '{"boardId":"6512f0a1c2d3e4f5a6b70001","actionId":"6512f0a1c2d3e4f5a6b74001","actionType":"updateCard",'
    '"occurredAt":"2026-11-02T09:00:00.123Z","card":{"id":"6512f0a1c2d3e4f5a6b71001","isClosed":false},'
    '"changedFields":["closed"]}'
)


def _connection(board: TrelloBoard) -> Connection:
    credentials = Credentials(SecretString(API_KEY), SecretString(TOKEN))
    client = Client(
        Config(poll_interval_seconds=10),
        StaticCredentialProvider({REFERENCE: credentials}),
        httpx.AsyncClient(transport=board.transport()),
    )
    return new_connection(client, REFERENCE)


class Recorder:
    def __init__(self) -> None:
        self.events: list[TriggerEvent[CardChangedEvent]] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[CardChangedEvent]) -> None:
        self.events.append(event)


async def _wait_until(condition: Callable[[], Awaitable[bool]]) -> None:
    async with asyncio.timeout(10):
        while not await condition():
            await asyncio.sleep(0.01)


def test_client_requires_a_poll_interval_of_at_least_ten_seconds() -> None:
    with pytest.raises(ValueError, match="pollInterval must be at least 10s"):
        Client(Config(poll_interval_seconds=9), StaticCredentialProvider({}))
    assert Client(Config(poll_interval_seconds=0), StaticCredentialProvider({})).poll_interval_seconds == 30


def test_configuration_decoding_and_inbox_payloads_match_go() -> None:
    assert decode_card_changed_trigger_configuration(
        {"boardId": "AbCd1234"}
    ) == CardChangedTriggerConfiguration("AbCd1234")
    with pytest.raises(ValueError, match="unknown field"):
        decode_card_changed_trigger_configuration({"board": "AbCd1234"})
    payload = json.dumps(FIXTURE_EVENT)
    decoded = decode_card_changed_event_payload(payload, payload.encode())
    assert decoded.card.is_closed is False and decoded.changed_fields == ("closed",)
    encoded = dex.CodecRegistry().resolve(CardChangedEvent).encode(decoded)
    assert json.loads(str(encoded.data)) == FIXTURE_EVENT, "a Go replica decodes the inbox"
    with pytest.raises(ValueError):
        decode_card_changed_event_payload("[]", b"[]")
    binding = define_card_changed_trigger_binding(connection_name="trello-cards", binding_name="cards")
    assert binding.definition == CARD_CHANGED_TRIGGER_DEFINITION


async def test_runner_records_a_durable_baseline_then_delivers_through_the_inbox() -> None:
    board = TrelloBoard()
    board.create_card("Existing card")
    objects = InMemoryObjectStore()
    key = TriggerInboxKey("trello", REFERENCE.name, "cardChanged", "cards")
    cursor = PollCursorStore(objects, SCOPE, PollCursorKey.from_trigger_inbox_key(key))
    recorder = Recorder()
    inbox = TriggerInbox(objects, SCOPE, key)
    target = new_durable_trigger_target(inbox, recorder, decode_payload=decode_card_changed_event_payload)
    configuration = CardChangedTriggerConfiguration(board.board_id)
    runner = new_card_trigger_runner(
        CardTriggerRunnerConfig(
            _connection(board), [CardChangedTriggerRoute("cards", configuration, target, cursor)]
        )
    )
    cancellation = TriggerCancellation()
    running = asyncio.create_task(runner.run(TriggerContext(cancellation)))

    async def is_seeded() -> bool:
        return (await cursor.read_poll_cursor()).state is not None

    async def is_delivered() -> bool:
        return bool(recorder.events)

    try:
        await _wait_until(is_seeded)
        created = board.create_card("Created after the baseline")
        runner.wake_poller("cards")
        await _wait_until(is_delivered)
        assert [event.payload.card.id for event in recorder.events] == [created]
        state = (await cursor.read_poll_cursor()).state
        assert state is not None and json.loads(state.token)["boardId"] == board.board_id
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running


async def test_single_binding_trigger_baselines_then_delivers_the_next_action() -> None:
    board = TrelloBoard()
    existing = board.create_card("Existing card")
    recorder = Recorder()
    trigger = new_card_changed_trigger(
        connection=_connection(board),
        binding_name="cards",
        configuration=CardChangedTriggerConfiguration(board.board_id),
        target=recorder,
    )
    cancellation = TriggerCancellation()
    running = asyncio.create_task(trigger.run(TriggerContext(cancellation)))

    async def has_polled() -> bool:
        return bool(board.requests())

    async def is_delivered() -> bool:
        return bool(recorder.events)

    try:
        await _wait_until(has_polled)
        board.comment_on_card(existing, "Still open.")
        # The single-binding Trigger polls on its own schedule, at least ten seconds apart.
        async with asyncio.timeout(30):
            while not await is_delivered():
                await asyncio.sleep(0.05)
        assert recorder.events[0].payload.comment_text == "Still open."
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await asyncio.wait_for(running, 10)


class RecordingTriggerLogger:
    def __init__(self) -> None:
        self.records: list[str] = []

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        self.records.append(f"{message} {attributes!r}")


async def test_failed_polls_keep_reflected_credentials_out_of_trigger_logs() -> None:
    def reflect_credentials(request: httpx.Request) -> httpx.Response:
        return httpx.Response(429, json={"error": TOKEN, "message": API_KEY})

    credentials = Credentials(SecretString(API_KEY), SecretString(TOKEN))
    client = Client(
        Config(poll_interval_seconds=10),
        StaticCredentialProvider({REFERENCE: credentials}),
        httpx.AsyncClient(transport=httpx.MockTransport(reflect_credentials)),
    )
    logger = RecordingTriggerLogger()
    configuration = CardChangedTriggerConfiguration("AbCd1234")
    runner = new_card_trigger_runner(
        CardTriggerRunnerConfig(
            new_connection(client, REFERENCE),
            [CardChangedTriggerRoute("cards", configuration, Recorder())],
            logger,
        )
    )
    cancellation = TriggerCancellation()
    running = asyncio.create_task(runner.run(TriggerContext(cancellation)))

    async def has_logged() -> bool:
        return bool(logger.records)

    try:
        await _wait_until(has_logged)
        assert any("rate limited" in record for record in logger.records), logger.records
        assert all(API_KEY not in record and TOKEN not in record for record in logger.records), logger.records
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running


def test_runner_rejects_incomplete_routes() -> None:
    board = TrelloBoard()
    connection = _connection(board)
    configuration = CardChangedTriggerConfiguration(board.board_id)
    with pytest.raises(ValueError, match="routes are required"):
        new_card_trigger_runner(CardTriggerRunnerConfig(connection, []))
    with pytest.raises(ValueError, match="binding name and target"):
        new_card_trigger_runner(
            CardTriggerRunnerConfig(connection, [CardChangedTriggerRoute(" ", configuration, Recorder())])
        )
    with pytest.raises(ValueError, match="boardId"):
        new_card_trigger_runner(
            CardTriggerRunnerConfig(
                connection,
                [CardChangedTriggerRoute("cards", CardChangedTriggerConfiguration(""), Recorder())],
            )
        )


def _project(bindings: dict[str, Any]) -> Any:
    connection = ConnectionConfiguration(
        "trello",
        "trello-cards",
        "github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go",
        "trello",
        {"pollInterval": 15_000_000_000},
        "apiKey",
    )
    configuration = Configuration(
        SCOPE,
        1,
        (connection,),
        tuple(
            {
                "connectorId": "trello",
                "connectionName": "trello-cards",
                "triggerName": "cardChanged",
                "bindingName": name,
                "configuration": value,
            }
            for name, value in bindings.items()
        ),
    )
    objects = InMemoryObjectStore()
    return SimpleNamespace(
        configuration=configuration,
        connections=ConnectionStore(objects, SCOPE),
        trigger_inbox=lambda key: TriggerInbox(objects, SCOPE, key),
        poll_cursor_store=lambda key: PollCursorStore(objects, SCOPE, key),
    )


async def test_project_runner_requires_a_saved_board() -> None:
    project = _project(
        {"saved": {"boardId": "AbCd1234"}, "typo": {"board": "AbCd1234"}, "bad": {"boardId": "x"}}
    )
    http_client = httpx.AsyncClient()
    runner = await new_project_card_trigger_runner(
        project,
        "trello-cards",
        [ProjectCardChangedTriggerRoute("saved", Recorder())],
        http_client=http_client,
    )
    assert runner.connection.reference.name == "trello-cards"
    await runner.aclose()
    for routes, message in (
        ([], "routes are required"),
        ([ProjectCardChangedTriggerRoute("unsaved", Recorder())], "save the watched board"),
        ([ProjectCardChangedTriggerRoute("typo", Recorder())], "unknown field"),
        ([ProjectCardChangedTriggerRoute("bad", Recorder())], "boardId must be"),
        ([ProjectCardChangedTriggerRoute("saved", Recorder())] * 2, "duplicated"),
    ):
        with pytest.raises(ValueError, match=message):
            await new_project_card_trigger_runner(project, "trello-cards", routes, http_client=http_client)
    await http_client.aclose()
