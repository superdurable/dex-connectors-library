# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real dexcli dev verification of the poll Trigger kit, mirroring sdkgo polling_trigger integration tests.

Each simulated process is one PollRunner.run over project storage shared across restarts. The provider
is an in-process change log behind an httpx mock transport, and every event starts one Dex Flow.
"""

import asyncio
import json
import os
import tempfile
import time
from collections.abc import AsyncIterator, Awaitable, Callable, Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any
from uuid import uuid4

import dex
import httpx
import pytest

from dex_connectors.sdk import (
    ConnectionRef,
    TriggerBindingRef,
    TriggerCancellation,
    TriggerContext,
    TriggerDefinition,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerLogLevel,
    TriggerLogValue,
    TriggerRef,
    TriggerTarget,
    new_dex_flow_trigger_target,
)
from dex_connectors.sdk.pollingtrigger import (
    BindingPollerConfig,
    ChangePage,
    Cursor,
    CursorStore,
    FeedPollerConfig,
    Poller,
    PollRunner,
    PollRunnerConfig,
    PollSchedule,
    mark_cursor_invalid,
    mark_rate_limited,
    new_binding_poller,
    new_feed_member,
    new_feed_poller,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    KeyedSnapshotEntry,
    PendingTriggerEvent,
    PollCursorKey,
    PollCursorRecord,
    PollCursorState,
    PollCursorStore,
    PollFeedKey,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
)
from dex_connectors.sdk.providerhttp import parse_retry_after

SERVER_ADDRESS = os.environ.get("DEX_SERVER_ADDRESS", "127.0.0.1:19881")
SUFFIX = uuid4().hex
SCOPE = Scope("poll-trigger-tests", "live")
CONNECTION = ConnectionRef("changelog", "local")
DEFINITION = TriggerDefinition(TriggerRef("changelog", "itemChanged"), "Receive a change-log item.")
PollItemPayload = dict[str, Any]


@dataclass(frozen=True)
class PollItem:
    """One polled change, the Flow input and output."""

    item_id: str
    sequence: int


class CompletePollItem(dex.Step[PollItem]):
    """Completes the item Flow with its input."""

    def get_step_type(self) -> str:
        """Name the start Step."""
        return "CompletePollItem"

    def execute(self, context: dex.Context, input: PollItem) -> dex.StepDecision:
        """Finish immediately."""
        return dex.force_complete(input)


class PollItemFlow(dex.Flow[PollItem]):
    """Started once per polled change."""

    def get_flow_type(self) -> str:
        """Return a run-unique Flow type."""
        return f"SdkpythonPollItem{SUFFIX}"

    def get_steps(self) -> dex.StepList[PollItem]:
        """Register the start Step."""
        return dex.StepList.start_step(CompletePollItem())


POLL_ITEM_FLOW = PollItemFlow()


def decode_poll_item(payload_json: str, payload_bytes: bytes) -> PollItemPayload:
    """Decode a replayed change payload."""
    payload = json.loads(payload_json)
    if not isinstance(payload, dict):
        raise ValueError("stored change payload is not an object")
    return payload


@dataclass(slots=True)
class RecordingLogger:
    """Keeps every Trigger record across simulated processes."""

    records: list[tuple[TriggerLogLevel, str, dict[str, TriggerLogValue]]] = field(default_factory=list)

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        """Enable every level."""
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        """Keep one record."""
        self.records.append((level, message, dict(attributes)))

    def find(self, message: str, binding: str) -> list[dict[str, TriggerLogValue]]:
        """Return the attributes of the records with message for binding."""
        return [
            record[2]
            for record in self.records
            if record[1] == message and record[2].get("binding") == binding
        ]


@dataclass(frozen=True, slots=True)
class ChangeRequest:
    """One provider request the change log received."""

    path: str
    after: str
    at: float


class ChangeLogProvider:
    """Sequence-numbered changes with scripted failures, served through an httpx mock transport."""

    def __init__(self) -> None:
        self.changes: list[PollItemPayload] = []
        self.responses: list[tuple[int, str]] = []
        self.requests: list[ChangeRequest] = []

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer /latest and /changes, or the next scripted failure for /changes."""
        after = request.url.params.get("after", "")
        self.requests.append(ChangeRequest(request.url.path, after, time.monotonic()))
        if request.url.path == "/changes" and self.responses:
            status, retry_after = self.responses.pop(0)
            return httpx.Response(status, headers={"Retry-After": retry_after} if retry_after else {})
        if request.url.path == "/latest":
            return httpx.Response(200, json={"latest": len(self.changes)})
        if not after.isdigit() or int(after) > len(self.changes):
            return httpx.Response(410)
        end = min(int(after) + int(request.url.params["limit"]), len(self.changes))
        return httpx.Response(
            200, json={"changes": self.changes[int(after) : end], "hasMore": end < len(self.changes)}
        )

    def append_items(self, prefix: str, count: int) -> list[str]:
        """Add changes for new item IDs and return them."""
        item_ids = [f"{prefix}-{index}" for index in range(count)]
        for item_id in item_ids:
            self.changes.append({"itemId": item_id, "sequence": len(self.changes) + 1})
        return item_ids

    def change_requests(self) -> list[ChangeRequest]:
        """Return the /changes requests in arrival order."""
        return [request for request in self.requests if request.path == "/changes"]


@dataclass(frozen=True, slots=True)
class ChangeLogFetcher:
    """The connector-side fetcher: 410 marks the cursor invalid and 429 rate limits the connection."""

    client: httpx.AsyncClient
    page_size: int = 100

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Start at the latest sequence."""
        latest = await self._get(context, "/latest", {})
        return Cursor(str(latest["latest"]))

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[PollItemPayload]]:
        """Return the changes after cursor; event IDs are "<itemId>@<sequence>"."""
        listed = await self._get(context, "/changes", {"after": cursor.token, "limit": str(self.page_size)})
        changes: list[PollItemPayload] = listed["changes"]
        events = [
            TriggerEvent(f"{change['itemId']}@{change['sequence']}", datetime.now(UTC), change)
            for change in changes
        ]
        next_token = str(changes[-1]["sequence"]) if changes else cursor.token
        return ChangePage(events, Cursor(next_token), bool(listed["hasMore"]))

    async def _get(self, context: TriggerContext, path: str, params: dict[str, str]) -> dict[str, Any]:
        await wait_for_poll_request(context)
        response = await self.client.get(path, params=params)
        if response.status_code == 410:
            raise mark_cursor_invalid(RuntimeError("change log cursor expired"))
        if response.status_code == 429:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            raise mark_rate_limited(RuntimeError("change log rate limited"), timedelta(seconds=retry_after))
        response.raise_for_status()
        listed: dict[str, Any] = response.json()
        return listed


async def eventually(condition_met: Callable[[], Awaitable[bool] | bool], timeout: float = 30) -> None:
    """Poll a condition until it holds; convergence polling, never a fixed sleep."""
    async with asyncio.timeout(timeout):
        while True:
            result = condition_met()
            if await result if isinstance(result, Awaitable) else result:
                return
            await asyncio.sleep(0.05)


@dataclass(slots=True)
class PollProcess:
    """One simulated application process: one PollRunner.run."""

    cancellation: TriggerCancellation
    task: "asyncio.Task[None]"

    async def stop(self) -> None:
        """Cancel the run and wait for it to end; idempotent."""
        self.cancellation.cancel()
        try:
            await asyncio.wait_for(self.task, 30)
        except TriggerDeliveryAbortedError:
            pass


CursorWrapper = Callable[[CursorStore, TriggerCancellation], CursorStore]


class PollEnvironment:
    """One test's Dex Worker, Client, provider, and project storage, kept across simulated restarts."""

    def __init__(self, client: dex.AsyncClient, registry: dex.Registry, cache: Any) -> None:
        self.client = client
        self.registry = registry
        self.cache = cache
        self.objects = InMemoryObjectStore()
        self.provider = ChangeLogProvider()
        self.http = httpx.AsyncClient(
            transport=httpx.MockTransport(self.provider.handle), base_url="http://changelog"
        )
        self.logger = RecordingLogger()
        self.run_id = uuid4().hex[:12]
        self.delivered: dict[str, int] = {}
        self.processes: list[PollProcess] = []
        self._worker: Any = None
        self._worker_task: asyncio.Task[Any] | None = None

    async def start_worker(self) -> None:
        """Start a real AsyncWorker for the item Flow."""
        self._worker = dex.AsyncWorker(
            self.registry, self.cache, dex.WorkerOptions(server_address=SERVER_ADDRESS)
        )
        await self._worker.__aenter__()
        self._worker_task = asyncio.create_task(self._worker.start())
        await self._worker.wait_pull_registered(timeout=30)

    async def stop_worker(self) -> None:
        """Stop the Worker, as a Worker outage while the Trigger keeps polling."""
        if self._worker is None or self._worker_task is None:
            return
        await self._worker.stop()
        await self._worker_task
        await self._worker.__aexit__(None, None, None)
        self._worker, self._worker_task = None, None

    def flow_id(self, event_id: str) -> str:
        """Return the Flow ID one event starts."""
        return f"poll-trigger-{self.run_id}-{event_id.replace('@', '-')}"

    def item_flow_id(self, item_id: str, sequence: int) -> str:
        """Return the Flow ID of one item change."""
        return self.flow_id(f"{item_id}@{sequence}")

    def inbox_key(self, binding_name: str) -> TriggerInboxKey:
        """Return a binding's inbox key."""
        return TriggerInboxKey("changelog", CONNECTION.name, DEFINITION.trigger.trigger_name, binding_name)

    def flow_target(self) -> TriggerTarget[PollItemPayload]:
        """Start the item Flow and count consumed deliveries per event across processes."""
        start: TriggerTarget[PollItemPayload] = new_dex_flow_trigger_target(
            self.client,
            POLL_ITEM_FLOW,
            accepts_event=lambda event: True,
            resolve_flow_id=lambda event: self.flow_id(event.id),
            map_to_flow_input=lambda event: PollItem(event.payload["itemId"], event.payload["sequence"]),
            logger=self.logger,
        )
        environment = self

        class CountingTarget:
            async def handle_trigger(
                self, context: TriggerContext, event: TriggerEvent[PollItemPayload]
            ) -> None:
                await start.handle_trigger(context, event)
                environment.delivered[event.id] = environment.delivered.get(event.id, 0) + 1

        return CountingTarget()

    def cursor_store(self, binding_name: str) -> PollCursorStore:
        """Bind a binding's cursor in the shared project storage."""
        return PollCursorStore(
            self.objects, SCOPE, PollCursorKey.from_trigger_inbox_key(self.inbox_key(binding_name))
        )

    async def cursor_state(self, binding_name: str = "main") -> PollCursorState | None:
        """Read a binding's committed cursor."""
        return (await self.cursor_store(binding_name).read_poll_cursor()).state

    async def pending_events(self, binding_name: str = "main") -> tuple[PendingTriggerEvent, ...]:
        """Read a binding's pending inbox events."""
        return await TriggerInbox(self.objects, SCOPE, self.inbox_key(binding_name)).pending()

    def runner(self, *pollers: Poller[Any]) -> PollRunner:
        """Return a one-second runner without jitter."""
        schedule = PollSchedule(timedelta(seconds=1), jitter_fraction=0, maximum_backoff=timedelta(seconds=2))
        return PollRunner(PollRunnerConfig(CONNECTION, schedule, pollers, logger=self.logger))

    def start_process(self, page_size: int = 100, wrap_cursor: CursorWrapper | None = None) -> PollProcess:
        """Start one process for the main binding over fresh objects."""
        cancellation = TriggerCancellation()
        inbox = TriggerInbox(self.objects, SCOPE, self.inbox_key("main"))
        durable = new_durable_trigger_target(
            inbox, self.flow_target(), decode_payload=decode_poll_item, logger=self.logger
        )
        cursor: CursorStore = self.cursor_store("main")
        if wrap_cursor is not None:
            cursor = wrap_cursor(cursor, cancellation)
        poller = new_binding_poller(
            BindingPollerConfig(
                DEFINITION,
                TriggerBindingRef(CONNECTION, DEFINITION.trigger, "main"),
                cursor,
                ChangeLogFetcher(self.http, page_size),
                durable,
            )
        )
        process = PollProcess(
            cancellation, asyncio.create_task(self.runner(poller).run(TriggerContext(cancellation)))
        )
        self.processes.append(process)
        return process

    async def wait_for_seed(self) -> None:
        """Wait until the main binding's cursor is seeded."""

        async def is_seeded() -> bool:
            return await self.cursor_state() is not None

        await eventually(is_seeded, 20)

    async def seed_and_stop(self) -> None:
        """Run one process until it seeds the main binding's cursor, then stop it."""
        process = self.start_process()
        await self.wait_for_seed()
        await process.stop()

    async def wait_for_item_flow(self, item_id: str, sequence: int) -> None:
        """Wait until the Trigger started the item's Flow and it completed."""
        flow_id = self.item_flow_id(item_id, sequence)

        async def is_started() -> bool:
            try:
                await self.client.describe_flow(flow_id)
            except dex.FlowNotFoundError:
                return False
            return True

        await eventually(is_started, 60)
        result = await asyncio.wait_for(self.client.wait_for_flow(flow_id, timedelta(seconds=60)), 70)
        assert result.status == dex.FlowStatus.COMPLETED, flow_id

    async def is_flow_started(self, item_id: str, sequence: int) -> bool:
        """Report whether the item's Flow exists."""
        try:
            await self.client.describe_flow(self.item_flow_id(item_id, sequence))
        except dex.FlowNotFoundError:
            return False
        return True


@pytest.fixture
async def environment() -> AsyncIterator[PollEnvironment]:
    """Run a real Worker and Client against the local Dex stack, with fresh project storage."""
    cache_root = Path(__file__).parents[1] / ".dexcli-data"
    cache_root.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(dir=cache_root) as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 16 * 1024 * 1024))
        registry = dex.Registry([POLL_ITEM_FLOW], allow_async_handlers=True)
        try:
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=SERVER_ADDRESS)
            ) as client:
                polling = PollEnvironment(client, registry, cache)
                await polling.start_worker()
                try:
                    yield polling
                finally:
                    for process in polling.processes:
                        await process.stop()
                    await polling.stop_worker()
                    await polling.http.aclose()
        finally:
            cache.close()


async def test_delivers_only_new_changes_across_restarts(environment: PollEnvironment) -> None:
    """R1 and R2: no backlog on first start; changes made while down each start one Flow."""
    backlog = environment.provider.append_items(f"backlog-{environment.run_id}", 5)
    first = environment.start_process()
    await environment.wait_for_seed()
    fresh = environment.provider.append_items(f"fresh-{environment.run_id}", 3)
    for index, item_id in enumerate(fresh):
        await environment.wait_for_item_flow(item_id, 6 + index)
    for index, item_id in enumerate(backlog):
        assert not await environment.is_flow_started(item_id, 1 + index), item_id
    await first.stop()

    await environment.stop_worker()
    offline = environment.provider.append_items(f"offline-{environment.run_id}", 4)
    for _ in range(2):
        process = environment.start_process()

        async def is_current() -> bool:
            state = await environment.cursor_state()
            return state is not None and state.token == "12"

        await eventually(is_current)
        await process.stop()
    state = await environment.cursor_state()
    assert state is not None
    commits_while_down = state.commit_count
    await environment.start_worker()
    environment.start_process()
    for index, item_id in enumerate(offline):
        await environment.wait_for_item_flow(item_id, 9 + index)

    async def is_drained() -> bool:
        return not await environment.pending_events()

    await eventually(is_drained)
    state = await environment.cursor_state()
    assert state is not None and state.commit_count == commits_while_down, (
        "restarts and empty polls write nothing"
    )
    assert len(environment.logger.find("poll cursor seeded", "main")) == 1


class CommitHookCursor:
    """Runs hooks around the first commit after the seed."""

    def __init__(
        self,
        store: CursorStore,
        before_commit: Callable[[], None] | None = None,
        after_commit: Callable[[], None] | None = None,
    ) -> None:
        self.store = store
        self.before_commit = before_commit
        self.after_commit = after_commit
        self.has_fired = False

    async def read_poll_cursor(self) -> PollCursorRecord:
        """Read through."""
        return await self.store.read_poll_cursor()

    async def replace_poll_cursor(self, expected_revision: str, state: PollCursorState) -> PollCursorRecord:
        """Fire the hooks around the first non-seed commit."""
        should_fire = not self.has_fired and state.commit_count > 1
        self.has_fired = self.has_fired or should_fire
        if should_fire and self.before_commit is not None:
            self.before_commit()
            raise RuntimeError("simulated process crash")
        record = await self.store.replace_poll_cursor(expected_revision, state)
        if should_fire and self.after_commit is not None:
            self.after_commit()
        return record


@pytest.mark.parametrize(
    "scenario", ["crash between record and commit", "crash after commit before delivery"]
)
async def test_recovers_a_crash_around_the_commit(environment: PollEnvironment, scenario: str) -> None:
    """R3 and R4: an uncommitted page is refetched; a committed one is replayed from the inbox only."""
    await environment.seed_and_stop()
    item_ids = environment.provider.append_items(f"crash-{environment.run_id}", 3)

    def wrap(cursor: CursorStore, crash: TriggerCancellation) -> CursorStore:
        if scenario == "crash between record and commit":
            return CommitHookCursor(cursor, before_commit=crash.cancel)
        return CommitHookCursor(cursor, after_commit=crash.cancel)

    crashed = environment.start_process(wrap_cursor=wrap)
    with pytest.raises(TriggerDeliveryAbortedError):
        await asyncio.wait_for(crashed.task, 60)
    assert len(await environment.pending_events()) == 3, "every recorded event survives the crash"
    requests_before_restart = len(environment.provider.change_requests())

    environment.start_process()
    for index, item_id in enumerate(item_ids):
        await environment.wait_for_item_flow(item_id, 1 + index)
        assert environment.delivered.get(f"{item_id}@{1 + index}", 0) >= 1

    async def is_drained() -> bool:
        return not await environment.pending_events()

    await eventually(is_drained)
    await eventually(lambda: len(environment.provider.change_requests()) > requests_before_restart)
    after_restart = environment.provider.change_requests()[requests_before_restart:]
    if scenario == "crash after commit before delivery":
        assert all(request.after == "3" for request in after_restart), "the committed page is never refetched"
    else:
        assert after_restart[0].after == "0", "an uncommitted page is fetched again from the old cursor"


async def test_reseeds_an_expired_cursor(environment: PollEnvironment) -> None:
    """R5: a 410 reseeds, the reset is logged, and the gap is not delivered."""
    environment.provider.append_items(f"before-{environment.run_id}", 3)
    await environment.seed_and_stop()
    environment.provider.changes = []
    gap = environment.provider.append_items(f"gap-{environment.run_id}", 2)
    environment.start_process()

    async def is_reset() -> bool:
        state = await environment.cursor_state()
        return state is not None and state.reset_count == 1

    await eventually(is_reset)
    after = environment.provider.append_items(f"after-{environment.run_id}", 2)
    for index, item_id in enumerate(after):
        await environment.wait_for_item_flow(item_id, 3 + index)
    for index, item_id in enumerate(gap):
        assert environment.delivered.get(f"{item_id}@{1 + index}", 0) == 0
    reset = environment.logger.find("poll cursor invalid; reseeding", "main")
    assert [(record["reset_count"], record["error_type"]) for record in reset] == [(1, "CursorInvalidError")]
    state = await environment.cursor_state()
    assert state is not None and state.last_reset_at_unix_ms is not None


async def test_honors_retry_after(environment: PollEnvironment) -> None:
    """R6: the next provider request waits for Retry-After; elapsed time is the behavior under test."""
    await environment.seed_and_stop()
    environment.provider.responses.append((429, "3"))
    item_ids = environment.provider.append_items(f"limited-{environment.run_id}", 1)
    requests_before_start = len(environment.provider.change_requests())
    environment.start_process()
    await environment.wait_for_item_flow(item_ids[0], 1)
    requests = environment.provider.change_requests()[requests_before_start:]
    assert len(requests) >= 2
    assert requests[1].at - requests[0].at >= 3
    paused = environment.logger.find("poll rate limited; pausing connection", "main")
    assert [(record["delay_ms"], record["pollers"]) for record in paused] == [(3000, 1)]


class CommitObservingCursor:
    """Reports every successful commit's commit count."""

    def __init__(self, store: CursorStore, commit_counts: list[int]) -> None:
        self.store = store
        self.commit_counts = commit_counts

    async def read_poll_cursor(self) -> PollCursorRecord:
        """Read through."""
        return await self.store.read_poll_cursor()

    async def replace_poll_cursor(self, expected_revision: str, state: PollCursorState) -> PollCursorRecord:
        """Write through and record the commit count of a successful write."""
        record = await self.store.replace_poll_cursor(expected_revision, state)
        self.commit_counts.append(state.commit_count)
        return record


async def test_replicas_share_one_cursor(environment: PollEnvironment) -> None:
    """R7: two concurrent replicas never lose a change or regress the cursor."""
    await environment.seed_and_stop()
    commit_counts: list[int] = []

    def observe(cursor: CursorStore, crash: TriggerCancellation) -> CursorStore:
        return CommitObservingCursor(cursor, commit_counts)

    environment.start_process(page_size=10, wrap_cursor=observe)
    environment.start_process(page_size=10, wrap_cursor=observe)
    item_ids = environment.provider.append_items(f"replica-{environment.run_id}", 50)
    for index, item_id in enumerate(item_ids):
        await environment.wait_for_item_flow(item_id, 1 + index)

    async def is_drained() -> bool:
        return not await environment.pending_events()

    await eventually(is_drained)
    state = await environment.cursor_state()
    assert state is not None and state.token == "50"
    assert len(commit_counts) == len(set(commit_counts)), "no commit count is written twice"


async def test_feed_serves_two_bindings(environment: PollEnvironment) -> None:
    """R8: one feed with a durable Flow member and a non-durable member, each getting its own events."""
    odd_inbox = TriggerInbox(environment.objects, SCOPE, environment.inbox_key("odd"))
    durable = new_durable_trigger_target(
        odd_inbox, environment.flow_target(), decode_payload=decode_poll_item, logger=environment.logger
    )

    def select_sequences(
        is_odd: bool,
    ) -> Callable[[TriggerEvent[PollItemPayload], KeyedSnapshotEntry | None], list[Any]]:
        def select(change: TriggerEvent[PollItemPayload], previous: KeyedSnapshotEntry | None) -> list[Any]:
            return [change] if (change.payload["sequence"] % 2 == 1) == is_odd else []

        return select

    even_events: list[str] = []

    class EvenTarget:
        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[PollItemPayload]) -> None:
            even_events.append(event.id)

    odd_member = new_feed_member(
        DEFINITION, TriggerBindingRef(CONNECTION, DEFINITION.trigger, "odd"), select_sequences(True), durable
    )
    even_member = new_feed_member(
        DEFINITION,
        TriggerBindingRef(CONNECTION, DEFINITION.trigger, "even"),
        select_sequences(False),
        EvenTarget(),
    )
    feed_cursor = PollCursorStore(
        environment.objects, SCOPE, PollFeedKey("changelog", CONNECTION.name, "itemFeed")
    )
    poller = new_feed_poller(
        FeedPollerConfig(
            "itemFeed", feed_cursor, ChangeLogFetcher(environment.http), [odd_member, even_member]
        )
    )
    cancellation = TriggerCancellation()
    running = asyncio.create_task(environment.runner(poller).run(TriggerContext(cancellation)))

    async def is_seeded() -> bool:
        return (await feed_cursor.read_poll_cursor()).state is not None

    await eventually(is_seeded, 20)
    item_ids = environment.provider.append_items(f"feed-{environment.run_id}", 4)
    for sequence in (1, 3):
        await environment.wait_for_item_flow(item_ids[sequence - 1], sequence)
    await eventually(lambda: len(even_events) == 2)
    assert even_events == [f"{item_ids[1]}@2", f"{item_ids[3]}@4"]
    for sequence in (2, 4):
        assert environment.delivered.get(f"{item_ids[sequence - 1]}@{sequence}", 0) == 0
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running
    assert len(even_events) == 2
