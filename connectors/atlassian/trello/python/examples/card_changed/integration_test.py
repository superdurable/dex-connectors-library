# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the cardChanged example against a real Dex Worker and Client and a local Trello board stand-in."""

import asyncio
import contextlib
import os
import tempfile
from collections.abc import AsyncIterator, Callable
from dataclasses import dataclass, field
from datetime import timedelta
from pathlib import Path

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
    new_dex_flow_trigger_target,
)
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    PollCursorKey,
    PollCursorState,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
)

from dex_connectors.atlassian_trello import (
    CardChangedEvent,
    CardChangedTriggerConfiguration,
    CardChangedTriggerRoute,
    CardTriggerRunnerConfig,
    Client,
    Config,
    Connection,
    Credentials,
    decode_card_changed_event_payload,
    new_card_trigger_runner,
    new_connection,
)
from tests.trello_board_fake import API_KEY, TOKEN, TrelloBoard

from .flow import (
    CARD_CHANGED_TRIGGER_BINDING,
    CONNECTION_NAME,
    CardChangeFlow,
    CardChangeOutcome,
    accepts_every_card_change,
    map_to_flow_input,
    resolve_flow_id,
)

SCOPE = Scope("trello-card-changed", "live")
INBOX_KEY = TriggerInboxKey("trello", CONNECTION_NAME, "cardChanged", CARD_CHANGED_TRIGGER_BINDING)


@dataclass
class TriggerProcess:
    """One run of the application's Trigger side; stopping it models a process exit."""

    runner: PollRunner
    task: "asyncio.Task[None]"
    cancellation: TriggerCancellation

    async def stop(self) -> None:
        self.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await self.task


@dataclass
class Environment:
    """The fake board, the Flow's real Dex Worker and Client, and storage that survives processes."""

    board: TrelloBoard
    flow: CardChangeFlow
    connection: Connection
    client: dex.AsyncClient
    objects: InMemoryObjectStore = field(default_factory=InMemoryObjectStore)
    delivered: dict[str, TriggerEvent[CardChangedEvent]] = field(default_factory=dict)
    processes: list[TriggerProcess] = field(default_factory=list)

    def cursor_store(self) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))

    def start_trigger_process(self, board_id: str) -> TriggerProcess:
        """Wire the example's target behind the durable inbox and cursor, as the project runner does."""
        start = new_dex_flow_trigger_target(
            self.client,
            self.flow,
            accepts_event=accepts_every_card_change,
            resolve_flow_id=resolve_flow_id,
            map_to_flow_input=map_to_flow_input,
        )
        environment = self

        class CountingTarget:
            async def handle_trigger(
                self, context: TriggerContext, event: TriggerEvent[CardChangedEvent]
            ) -> None:
                await start.handle_trigger(context, event)
                environment.delivered[event.id] = event

        target = new_durable_trigger_target(
            TriggerInbox(self.objects, SCOPE, INBOX_KEY),
            CountingTarget(),
            decode_payload=decode_card_changed_event_payload,
        )
        route = CardChangedTriggerRoute(
            CARD_CHANGED_TRIGGER_BINDING,
            CardChangedTriggerConfiguration(board_id),
            target,
            self.cursor_store(),
        )
        runner = new_card_trigger_runner(CardTriggerRunnerConfig(self.connection, [route]))
        cancellation = TriggerCancellation()
        process = TriggerProcess(
            runner, asyncio.create_task(runner.run(TriggerContext(cancellation))), cancellation
        )
        self.processes.append(process)
        return process

    async def wait_for_cursor(self, is_ready: Callable[[PollCursorState | None], bool]) -> None:
        async with asyncio.timeout(30):
            while not is_ready((await self.cursor_store().read_poll_cursor()).state):
                await asyncio.sleep(0.05)

    async def wait_for_outcomes(self, count: int) -> list[CardChangeOutcome]:
        """Wait for count delivered actions, then each action's Flow, in delivery order."""
        async with asyncio.timeout(30):
            while len(self.delivered) < count:
                await asyncio.sleep(0.05)
        outcomes = []
        for event in list(self.delivered.values()):
            result = await self.client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(CardChangeOutcome)
            assert (outcome.card_id, outcome.action_type) == (
                event.payload.card.id,
                event.payload.action_type,
            )
            outcomes.append(outcome)
        return outcomes


@contextlib.asynccontextmanager
async def card_changed_environment() -> AsyncIterator[Environment]:
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    board = TrelloBoard()
    reference = ConnectionRef("trello", CONNECTION_NAME)
    connection = new_connection(
        Client(
            Config(endpoint="http://127.0.0.1:12345/1", poll_interval_seconds=10),
            StaticCredentialProvider({reference: Credentials(SecretString(API_KEY), SecretString(TOKEN))}),
            httpx.AsyncClient(transport=board.transport()),
        ),
        reference,
    )
    flow = CardChangeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    with tempfile.TemporaryDirectory(prefix="trello-python-card-changed-") as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                environment = Environment(board, flow, connection, client)
                try:
                    yield environment
                finally:
                    for process in environment.processes:
                        process.cancellation.cancel()
                        with contextlib.suppress(TriggerDeliveryAbortedError):
                            await process.task
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()


async def test_card_actions_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    async with card_changed_environment() as environment:
        board = environment.board
        existing = board.create_card("Existing card")
        first = environment.start_trigger_process(board.board_id)
        await environment.wait_for_cursor(lambda state: state is not None)

        created = board.create_card("Replace badge reader")
        board.move_card(created)
        first.runner.wake_poller(CARD_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(2)
        assert [outcome.action_type for outcome in outcomes] == ["createCard", "updateCard"]
        assert (
            outcomes[1].current is not None and outcomes[1].current.list_id == board.cards[created]["idList"]
        )
        await first.stop()

        board.comment_on_card(existing, "Filed while stopped.")
        board.delete_card(existing)
        board.rename_card(created, "Replace badge reader (east)")
        second = environment.start_trigger_process(board.board_id)
        second.runner.wake_poller(CARD_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(5)
        commented, deleted, renamed = outcomes[2:]
        assert commented.action_type == "commentCard" and commented.current is None, (
            "a card deleted after its action is recorded without a current state"
        )
        assert deleted.action_type == "deleteCard" and deleted.current is None
        assert renamed.current is not None and renamed.current.name == "Replace badge reader (east)"
        requests_before = len(board.requests())
        second.runner.wake_poller(CARD_CHANGED_TRIGGER_BINDING)
        async with asyncio.timeout(30):
            while len(board.requests()) <= requests_before:
                await asyncio.sleep(0.05)
        assert len(environment.delivered) == 5, "no action is delivered twice and the baseline never is"


async def test_rate_limit_pauses_then_delivers_with_real_dex() -> None:
    async with card_changed_environment() as environment:
        board = environment.board
        process = environment.start_trigger_process(board.board_id)
        await environment.wait_for_cursor(lambda state: state is not None)
        board.rate_limit_next_action_requests(1)
        created = board.create_card("Filed during a rate limit")
        process.runner.wake_poller(CARD_CHANGED_TRIGGER_BINDING)
        [outcome] = await environment.wait_for_outcomes(1)
        assert outcome.card_id == created
        assert 429 in [request.status for request in board.requests()]


async def test_changed_board_records_a_new_baseline_with_real_dex() -> None:
    async with card_changed_environment() as environment:
        board = environment.board
        first = environment.start_trigger_process(board.board_id)
        await environment.wait_for_cursor(lambda state: state is not None)
        await first.stop()
        other = TrelloBoard()
        board.board_id, other_board = other.board_id, board.board_id
        gap = board.create_card("Existing on the new board")
        second = environment.start_trigger_process(board.board_id)
        await environment.wait_for_cursor(lambda state: state is not None and state.reset_count == 1)
        after = board.create_card("After the reset")
        second.runner.wake_poller(CARD_CHANGED_TRIGGER_BINDING)
        [outcome] = await environment.wait_for_outcomes(1)
        assert outcome.card_id == after and outcome.board_id == board.board_id != other_board
        assert gap not in [event.payload.card.id for event in environment.delivered.values()]
