# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll Trigger kit ordering, recovery, schedule, and wiring, ported from sdkgo/pollingtrigger tests.

A fake clock replaces the kit's clock, jitter, and wait, so schedules are observed without sleeping.
"""

import asyncio
import gc
import importlib.util
import threading
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import pytest

from dex_connectors.sdk import (
    ConnectionRef,
    TriggerBindingRef,
    TriggerCancellation,
    TriggerConfig,
    TriggerContext,
    TriggerDefinition,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerLogLevel,
    TriggerLogValue,
    TriggerRef,
    TriggerTarget,
    new_trigger,
)
from dex_connectors.sdk.pollingtrigger import (
    BindingPollerConfig,
    ChangePage,
    Cursor,
    CursorStore,
    FeedEventSelector,
    FeedMember,
    FeedPollerConfig,
    KeyedSnapshotOptions,
    Poller,
    PollRunner,
    PollRunnerConfig,
    PollSchedule,
    RateLimitedError,
    apply_keyed_snapshot_changes,
    diff_keyed_snapshot,
    is_cursor_invalid,
    mark_cursor_invalid,
    mark_rate_limited,
    new_binding_poller,
    new_binding_trigger_source,
    new_feed_member,
    new_feed_poller,
    new_memory_cursor_store,
    wait_for_poll_request,
)
from dex_connectors.sdk.pollingtrigger import schedule as poll_schedule
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    KeyedSnapshot,
    KeyedSnapshotEntry,
    PollCursorDocumentInvalidError,
    PollCursorKey,
    PollCursorRecord,
    PollCursorState,
    PollCursorStore,
    PollCursorTooLargeError,
    Scope,
)

CONNECTION = ConnectionRef("changelog", "main")
START = datetime(2026, 11, 15, 12, 0, 0, tzinfo=UTC)


def definition(trigger_name: str) -> TriggerDefinition:
    return TriggerDefinition(TriggerRef("changelog", trigger_name), "Receive a change.")


def binding(trigger_name: str, binding_name: str) -> TriggerBindingRef:
    return TriggerBindingRef(CONNECTION, definition(trigger_name).trigger, binding_name)


@dataclass(frozen=True, slots=True)
class Item:
    sequence: int
    title: str


@dataclass(slots=True)
class OperationLog:
    """The order of storage, provider, and target operations."""

    operations: list[str] = field(default_factory=list)

    def add(self, operation: str) -> None:
        self.operations.append(operation)

    def count(self, operation: str) -> int:
        return self.operations.count(operation)


@dataclass(slots=True)
class RecordingLogger:
    """Keeps every Trigger record."""

    records: list[tuple[TriggerLogLevel, str, dict[str, TriggerLogValue]]] = field(default_factory=list)

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        self.records.append((level, message, dict(attributes)))


class FakeClock:
    """Replaces the kit clock, jitter, and wait; `stop_when` cancels the run at a wait."""

    def __init__(self, monkeypatch: pytest.MonkeyPatch, jitter: float) -> None:
        self.current = START
        self.waits: list[timedelta] = []
        self.stop_when: Callable[[], bool] | None = None
        self.cancellation: TriggerCancellation | None = None
        monkeypatch.setattr(poll_schedule, "_now", lambda: self.current)
        monkeypatch.setattr(poll_schedule, "_jitter_source", lambda: jitter)
        monkeypatch.setattr(poll_schedule, "_wait_for_poll", self.wait)

    async def wait(self, cancellation: TriggerCancellation, delay: timedelta, wake: asyncio.Event) -> None:
        self.waits.append(delay)
        await asyncio.sleep(0)
        if len(self.waits) > 500 or (self.stop_when is not None and self.stop_when()):
            cancellation.cancel()
        else:
            self.current += delay
        cancellation.raise_if_cancelled()

    async def run_until(self, runner: PollRunner, stop: Callable[[], bool]) -> None:
        """Run runner until stop holds at a wait; the run ends with TriggerDeliveryAbortedError."""
        self.stop_when = stop
        with pytest.raises(TriggerDeliveryAbortedError):
            await runner.run(TriggerContext(TriggerCancellation()))


class ChangeLogFetcher:
    """A scripted sequence-numbered provider feed."""

    def __init__(self, log: OperationLog, name: str, page_size: int = 1) -> None:
        self.log = log
        self.name = name
        self.page_size = page_size
        self.changes: list[Item] = []
        self.failures: list[BaseException | None] = []
        self.seed_failures: list[BaseException | None] = []
        self.on_fetch: Callable[[TriggerContext], None] | None = None

    def append(self, *titles: str) -> None:
        for title in titles:
            self.changes.append(Item(len(self.changes) + 1, title))

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        await wait_for_poll_request(context)
        self.log.add(f"seed {self.name}")
        if self.seed_failures:
            failure = self.seed_failures.pop(0)
            if failure is not None:
                raise failure
        return Cursor(str(len(self.changes)))

    async def fetch_changes(self, context: TriggerContext, cursor: Cursor) -> ChangePage[TriggerEvent[Item]]:
        await wait_for_poll_request(context)
        if self.on_fetch is not None:
            self.on_fetch(context)
            context.cancellation.raise_if_cancelled()
        self.log.add(f"fetch {self.name} after {cursor.token}")
        if self.failures:
            failure = self.failures.pop(0)
            if failure is not None:
                raise failure
        if not cursor.token.isdigit():
            raise mark_cursor_invalid(ValueError("cursor is not a sequence"))
        after = int(cursor.token)
        end = min(after + max(self.page_size, 1), len(self.changes))
        return ChangePage(
            [TriggerEvent(change.title, START, change) for change in self.changes[after:end]],
            Cursor(str(end)),
            end < len(self.changes),
        )


class LoggedCursorStore:
    """Records reads and replaces and can fail or race the next replace."""

    def __init__(self, store: CursorStore, log: OperationLog) -> None:
        self.store = store
        self.log = log
        self.replace_error: BaseException | None = None
        self.before_commit: Callable[[], Any] | None = None

    async def read_poll_cursor(self) -> PollCursorRecord:
        self.log.add("read")
        return await self.store.read_poll_cursor()

    async def replace_poll_cursor(self, expected_revision: str, state: PollCursorState) -> PollCursorRecord:
        if self.before_commit is not None:
            hook, self.before_commit = self.before_commit, None
            await hook()
        self.log.add(f"replace {state.token}")
        if self.replace_error is not None:
            error, self.replace_error = self.replace_error, None
            raise error
        return await self.store.replace_poll_cursor(expected_revision, state)

    async def state(self) -> PollCursorState:
        record = await self.store.read_poll_cursor()
        assert record.state is not None
        return record.state


class RecordingTarget:
    """A non-durable target."""

    def __init__(
        self, log: OperationLog, name: str = "", handle_errors: dict[str, list[Exception]] | None = None
    ):
        self.log = log
        self.name = name
        self.handle_errors = handle_errors or {}

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        self.log.add(f"handle {self.name}{event.id}")
        failures = self.handle_errors.get(event.id)
        if failures:
            raise failures.pop(0)


class DurableTarget(RecordingTarget):
    """A target that records events before the commit and replays them."""

    def __init__(
        self,
        log: OperationLog,
        name: str = "",
        handle_errors: dict[str, list[Exception]] | None = None,
        prepare_errors: dict[str, list[Exception]] | None = None,
    ):
        super().__init__(log, name, handle_errors)
        self.prepare_errors = prepare_errors or {}
        self.replayed = 0

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        self.log.add(f"prepare {self.name}{event.id}")
        failures = self.prepare_errors.get(event.id)
        if failures:
            raise failures.pop(0)

    async def replay_trigger_deliveries(self, context: TriggerContext) -> None:
        self.replayed += 1
        self.log.add(f"replay {self.name}")


def new_runner(
    *pollers: Poller[Any],
    schedule: PollSchedule | None = None,
    logger: RecordingLogger | None = None,
    **options: Any,
) -> PollRunner:
    return PollRunner(
        PollRunnerConfig(
            CONNECTION, schedule or PollSchedule(timedelta(minutes=1)), pollers, logger=logger, **options
        )
    )


def new_poller(
    binding_name: str, store: CursorStore, fetcher: ChangeLogFetcher, target: TriggerTarget[Item]
) -> Poller[TriggerEvent[Item]]:
    return new_binding_poller(
        BindingPollerConfig(
            definition("itemChanged"), binding("itemChanged", binding_name), store, fetcher, target
        )
    )


async def seed(store: CursorStore, token: str) -> None:
    record = await store.read_poll_cursor()
    await store.replace_poll_cursor(record.revision, PollCursorState(token, commit_count=1))


async def test_binding_poller_seeds_once_without_backlog(monkeypatch: pytest.MonkeyPatch) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    fetcher = ChangeLogFetcher(log, "feed", 10)
    fetcher.append("old-1", "old-2", "old-3")
    target = DurableTarget(log)
    await clock.run_until(
        new_runner(new_poller("main", store, fetcher, target)), lambda: log.count("fetch feed after 3") == 1
    )
    assert log.operations == [
        "read",
        "replay ",
        "read",
        "seed feed",
        "replace 3",
        "read",
        "fetch feed after 3",
    ]
    assert (await store.state()).commit_count == 1, "an empty page writes nothing"
    # The first poll waits a uniform share of interval*jitter_fraction; jitter 0.5 is the midpoint.
    assert clock.waits[0] == timedelta(seconds=3)
    fetcher.append("new-1")
    await clock.run_until(
        new_runner(new_poller("main", store, fetcher, target)), lambda: log.count("handle new-1") == 1
    )
    assert log.count("seed feed") == 1


@pytest.mark.parametrize("is_durable", [True, False], ids=["durable target", "non-durable target"])
async def test_binding_poller_records_before_commit_and_delivers_after(
    monkeypatch: pytest.MonkeyPatch, is_durable: bool
) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    await seed(store, "0")
    fetcher = ChangeLogFetcher(log, "feed", 10)
    fetcher.append("a", "b")
    target = DurableTarget(log) if is_durable else RecordingTarget(log)
    await clock.run_until(
        new_runner(new_poller("main", store, fetcher, target)), lambda: log.count("replace 2") == 1
    )
    expected = (
        ["read", "fetch feed after 0", "prepare a", "prepare b", "replace 2", "handle a", "handle b"]
        if is_durable
        else ["read", "fetch feed after 0", "handle a", "handle b", "replace 2"]
    )
    assert log.operations[-len(expected) :] == expected


async def test_binding_poller_retries_a_record_failure_from_the_old_cursor(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from dex_connectors.sdk.projectconfig import TriggerInboxFullError

    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    await seed(store, "0")
    fetcher = ChangeLogFetcher(log, "feed", 10)
    fetcher.append("a", "b")
    target = DurableTarget(log, prepare_errors={"b": [TriggerInboxFullError()]})
    await clock.run_until(
        new_runner(new_poller("main", store, fetcher, target)), lambda: log.count("handle b") == 1
    )
    assert log.operations[4:] == [
        "read", "fetch feed after 0", "prepare a", "prepare b",
        "read", "fetch feed after 0", "prepare b", "replace 2", "handle a", "handle b",
    ]  # fmt: skip
    # The failed poll waits one interval of backoff before the retry.
    assert clock.waits[1] == timedelta(minutes=1)


async def test_binding_poller_retries_a_failed_event_before_fetching_again(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    await seed(store, "0")
    fetcher = ChangeLogFetcher(log, "feed", 10)
    fetcher.append("a", "b")
    failure = RuntimeError("Dex is unavailable")
    target = DurableTarget(log, handle_errors={"a": [failure, failure]})
    await clock.run_until(
        new_runner(new_poller("main", store, fetcher, target)), lambda: log.count("fetch feed after 2") == 1
    )
    assert log.operations[4:] == [
        "read", "fetch feed after 0", "prepare a", "prepare b", "replace 2", "handle a",
        "handle a", "handle a", "handle b", "read", "fetch feed after 2",
    ]  # fmt: skip
    assert clock.waits[1:4] == [timedelta(minutes=1), timedelta(minutes=2), timedelta(minutes=1)]


async def test_binding_poller_delivers_recorded_events_after_a_commit_conflict(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    memory = new_memory_cursor_store()
    store = LoggedCursorStore(memory, log)
    await seed(store, "0")

    async def win_the_race() -> None:
        record = await memory.read_poll_cursor()
        assert record.state is not None
        winner = PollCursorState("1", commit_count=record.state.commit_count + 1)
        await memory.replace_poll_cursor(record.revision, winner)

    store.before_commit = win_the_race
    fetcher = ChangeLogFetcher(log, "feed", 10)
    fetcher.append("a", "b")
    target = DurableTarget(log)
    await clock.run_until(
        new_runner(new_poller("main", store, fetcher, target)), lambda: log.count("fetch feed after 1") == 1
    )
    assert log.operations[4:] == [
        "read", "fetch feed after 0", "prepare a", "prepare b", "replace 2", "handle a", "handle b",
        "read", "fetch feed after 1", "prepare b", "replace 2", "handle b",
    ]  # fmt: skip


async def test_binding_poller_reseeds_an_invalid_cursor(monkeypatch: pytest.MonkeyPatch) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    await seed(store, "0")
    fetcher = ChangeLogFetcher(log, "feed", 10)
    fetcher.failures = [mark_cursor_invalid(RuntimeError("410 Gone"))]
    fetcher.append("gap-1", "gap-2")
    logger = RecordingLogger()
    await clock.run_until(
        new_runner(new_poller("main", store, fetcher, DurableTarget(log)), logger=logger),
        lambda: log.count("fetch feed after 2") == 1,
    )
    assert log.operations[4:] == [
        "read",
        "fetch feed after 0",
        "seed feed",
        "replace 2",
        "read",
        "fetch feed after 2",
    ]
    state = await store.state()
    assert state.reset_count == 1
    assert (
        state.last_reset_at_unix_ms
        == state.seeded_at_unix_ms
        == poll_schedule._unix_milliseconds(START + timedelta(seconds=3))
    )
    assert log.count("prepare gap-1") == 0
    reset = [record for record in logger.records if record[1] == "poll cursor invalid; reseeding"]
    assert [(record[0], record[2]["reset_count"], record[2]["error_type"]) for record in reset] == [
        (TriggerLogLevel.WARN, 1, "CursorInvalidError")
    ]


def replace_changes_on_fetch(fetcher: ChangeLogFetcher, fetch_number: int, *titles: str) -> None:
    """Make that fetch see titles as the whole feed, as a listing that changed before a retry."""
    fetches = 0

    def on_fetch(context: TriggerContext) -> None:
        nonlocal fetches
        fetches += 1
        if fetches == fetch_number:
            fetcher.changes = []
            fetcher.append(*titles)

    fetcher.on_fetch = on_fetch


async def test_retry_skips_consumed_events_by_id_when_the_page_changes(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    await seed(store, "0")
    fetcher = ChangeLogFetcher(log, "feed", 10)
    fetcher.append("a", "b", "c")
    # The retry from cursor 0 no longer lists a, so b and c move up one position.
    replace_changes_on_fetch(fetcher, 2, "b", "c", "d")
    target = RecordingTarget(log, handle_errors={"c": [RuntimeError("Dex is unavailable")]})
    await clock.run_until(
        new_runner(new_poller("main", store, fetcher, target)), lambda: log.count("replace 3") == 1
    )
    assert log.operations[3:] == [
        "read", "fetch feed after 0", "handle a", "handle b", "handle c",
        "read", "fetch feed after 0", "handle c", "handle d", "replace 3",
    ]  # fmt: skip


async def test_recorded_event_the_retry_no_longer_lists_is_delivered(monkeypatch: pytest.MonkeyPatch) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    await seed(store, "0")
    fetcher = ChangeLogFetcher(log, "feed", 10)
    fetcher.append("a", "b")
    replace_changes_on_fetch(fetcher, 2, "b")
    target = DurableTarget(log, prepare_errors={"b": [RuntimeError("inbox write failed")]})
    await clock.run_until(
        new_runner(new_poller("main", store, fetcher, target)), lambda: log.count("handle b") == 1
    )
    assert log.operations[4:] == [
        "read", "fetch feed after 0", "prepare a", "prepare b",
        "read", "fetch feed after 0", "prepare b", "replace 1", "handle a", "handle b",
    ]  # fmt: skip


async def test_recorded_events_are_delivered_when_the_retry_page_is_empty(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    await seed(store, "0")
    fetcher = ChangeLogFetcher(log, "feed", 10)
    fetcher.append("a", "b")
    replace_changes_on_fetch(fetcher, 2)
    target = DurableTarget(log, prepare_errors={"b": [RuntimeError("inbox write failed")]})
    await clock.run_until(
        new_runner(new_poller("main", store, fetcher, target)), lambda: log.count("handle a") == 1
    )
    assert log.operations[4:] == [
        "read", "fetch feed after 0", "prepare a", "prepare b", "read", "fetch feed after 0", "handle a",
    ]  # fmt: skip


@dataclass(frozen=True, slots=True)
class FileChange:
    """A delta-feed change the feed snapshot tracks by file ID."""

    file_id: str
    version: int

    def snapshot_key(self) -> str:
        return self.file_id

    def snapshot_hash(self) -> str:
        return f"{self.version:016x}"

    def snapshot_value(self) -> bytes | None:
        return None


class FileFeedFetcher:
    """Returns scripted pages and keeps the snapshot with apply_keyed_snapshot_changes."""

    def __init__(self, log: OperationLog, pages: list[list[FileChange]]) -> None:
        self.log = log
        self.pages = pages

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        return Cursor("0", KeyedSnapshot())

    async def fetch_changes(self, context: TriggerContext, cursor: Cursor) -> ChangePage[FileChange]:
        self.log.add(f"fetch after {cursor.token}")
        index = int(cursor.token)
        if index >= len(self.pages):
            return ChangePage((), cursor)
        snapshot = apply_keyed_snapshot_changes(
            cursor.snapshot, self.pages[index], (), KeyedSnapshotOptions()
        )
        return ChangePage(self.pages[index], Cursor(str(index + 1), snapshot))


def select_file_events(kind: str) -> FeedEventSelector[FileChange, Item]:
    def select(change: FileChange, previous: KeyedSnapshotEntry | None) -> list[TriggerEvent[Item]]:
        if (kind == "created") != (previous is None):
            return []
        return [TriggerEvent(f"{change.file_id}@{change.version}", START, Item(change.version, ""))]

    return select


def file_member(kind: str, target: TriggerTarget[Item]) -> FeedMember[FileChange]:
    trigger_name = "fileCreated" if kind == "created" else "fileUpdated"
    return new_feed_member(
        definition(trigger_name), binding(trigger_name, kind), select_file_events(kind), target
    )


async def test_feed_poller_shares_one_fetch_and_skips_recorded_members_on_retry(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    fetcher = FileFeedFetcher(log, [[FileChange("f1", 1)], [FileChange("f1", 2), FileChange("f2", 1)]])
    created = DurableTarget(log, "created:")
    updated = DurableTarget(log, "updated:", prepare_errors={"f1@2": [RuntimeError("inbox write failed")]})
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    poller = new_feed_poller(
        FeedPollerConfig(
            "fileChanges", store, fetcher, [file_member("created", created), file_member("updated", updated)],
            KeyedSnapshotOptions(),
        )
    )  # fmt: skip
    await clock.run_until(new_runner(poller), lambda: log.count("fetch after 2") == 1)
    assert log.operations == [
        "read", "replay created:", "replay updated:", "read", "replace 0",
        "read", "fetch after 0", "prepare created:f1@1", "replace 1", "handle created:f1@1",
        "read", "fetch after 1", "prepare created:f2@1", "prepare updated:f1@2",
        "read", "fetch after 1", "prepare updated:f1@2", "replace 2",
        "handle created:f2@1", "handle updated:f1@2",
        "read", "fetch after 2",
    ]  # fmt: skip
    state = await store.state()
    assert state.snapshot is not None
    assert state.snapshot.entries == (
        KeyedSnapshotEntry("f1", "0000000000000002", 3),
        KeyedSnapshotEntry("f2", "0000000000000001", 3),
    )


@dataclass(frozen=True, slots=True)
class Row:
    key: str
    hash: str
    value: bytes | None = None

    def snapshot_key(self) -> str:
        return self.key

    def snapshot_hash(self) -> str:
        return self.hash

    def snapshot_value(self) -> bytes | None:
        return self.value


def test_diff_keyed_snapshot_reports_added_changed_and_removed_keys() -> None:
    previous = KeyedSnapshot(
        (
            KeyedSnapshotEntry("a", "000000000000000a", 3, b'"old a"'),
            KeyedSnapshotEntry("b", "000000000000000b", 4),
            KeyedSnapshotEntry("c", "000000000000000c", 5),
        )
    )
    rows = [
        Row("d", "000000000000000d", b'"d"'),
        Row("a", "00000000000000a2", b'"new a"'),
        Row("b", "000000000000000b"),
    ]
    diff = diff_keyed_snapshot(previous, rows, KeyedSnapshotOptions(keep_values=True))
    assert [entry.key for entry in diff.added] == ["d"]
    assert [(change.previous.value, change.current.value) for change in diff.changed] == [
        (b'"old a"', b'"new a"')
    ]
    assert [entry.key for entry in diff.removed] == ["c"]
    assert [(entry.key, entry.seen) for entry in diff.next_snapshot.entries] == [("a", 0), ("b", 4), ("d", 0)]
    with pytest.raises(ValueError):
        diff_keyed_snapshot(
            None, [Row("a", "000000000000000a"), Row("a", "000000000000000a")], KeyedSnapshotOptions()
        )
    with pytest.raises(PollCursorTooLargeError):
        diff_keyed_snapshot(
            None,
            [Row("a", "000000000000000a"), Row("b", "000000000000000b")],
            KeyedSnapshotOptions(max_entries=1),
        )
    with pytest.raises(ValueError):
        diff_keyed_snapshot(None, [], KeyedSnapshotOptions(evict_least_recently_seen=True))
    with pytest.raises(ValueError):
        diff_keyed_snapshot(None, [], KeyedSnapshotOptions(max_entries=200_000))
    unicode_rows = [
        Row("😀", "000000000000000a"),
        Row("ｚ", "000000000000000a"),
        Row("é", "000000000000000a"),
    ]
    keys = [
        entry.key
        for entry in diff_keyed_snapshot(None, unicode_rows, KeyedSnapshotOptions()).next_snapshot.entries
    ]
    assert keys == ["é", "ｚ", "😀"], "keys sort in UTF-8 byte order, as Go sorts them"


def test_apply_keyed_snapshot_changes_evicts_the_least_recently_seen() -> None:
    previous = KeyedSnapshot(
        (
            KeyedSnapshotEntry("a", "000000000000000a", 2),
            KeyedSnapshotEntry("b", "000000000000000b", 1),
            KeyedSnapshotEntry("c", "000000000000000c", 3),
        )
    )
    upserted = [Row("d", "000000000000000d")]
    with pytest.raises(PollCursorTooLargeError):
        apply_keyed_snapshot_changes(previous, upserted, (), KeyedSnapshotOptions(max_entries=3))
    evicted = apply_keyed_snapshot_changes(
        previous, upserted, ["c"], KeyedSnapshotOptions(max_entries=2, evict_least_recently_seen=True)
    )
    assert [entry.key for entry in evicted.entries] == ["a", "d"]
    assert len(previous.entries) == 3


async def test_poller_rejects_a_snapshot_over_its_bound(monkeypatch: pytest.MonkeyPatch) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    fetcher = FileFeedFetcher(log, [[FileChange("f1", 1), FileChange("f2", 1)]])
    poller = new_feed_poller(
        FeedPollerConfig(
            "fileChanges", new_memory_cursor_store(), fetcher, [file_member("created", RecordingTarget(log))],
            KeyedSnapshotOptions(max_entries=1),
        )
    )  # fmt: skip
    schedule = PollSchedule(timedelta(minutes=1), maximum_backoff=timedelta(minutes=7))
    await clock.run_until(new_runner(poller, schedule=schedule), lambda: log.count("fetch after 0") == 2)
    assert clock.waits[2] == timedelta(minutes=7)
    assert log.count("handle f1@1") == 0


async def test_rate_limit_pauses_every_poller_of_the_connection(monkeypatch: pytest.MonkeyPatch) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store_a = LoggedCursorStore(new_memory_cursor_store(), OperationLog())
    store_b = LoggedCursorStore(new_memory_cursor_store(), OperationLog())
    await seed(store_a, "0")
    await seed(store_b, "0")
    fetcher_a = ChangeLogFetcher(log, "a")
    fetcher_a.failures = [mark_rate_limited(RuntimeError("429"), timedelta(seconds=120))]
    fetcher_b = ChangeLogFetcher(log, "b")
    logger = RecordingLogger()
    runner = new_runner(
        new_poller("first", store_a, fetcher_a, RecordingTarget(log)),
        new_poller("second", store_b, fetcher_b, RecordingTarget(log)),
        logger=logger,
    )
    await clock.run_until(runner, lambda: log.count("fetch b after 0") == 1)
    assert log.operations == ["fetch a after 0", "fetch a after 0", "fetch b after 0"]
    assert clock.waits[:2] == [timedelta(seconds=3), timedelta(seconds=120)]
    paused = [record for record in logger.records if record[1] == "poll rate limited; pausing connection"]
    assert [
        (record[0], record[2]["delay_ms"], record[2]["pollers"], record[2]["binding"]) for record in paused
    ] == [(TriggerLogLevel.WARN, 120_000, 2, "first")]


async def test_rate_limit_without_retry_after_and_failures_back_off_exponentially(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), OperationLog())
    await seed(store, "0")
    failure = RuntimeError("503")
    fetcher = ChangeLogFetcher(log, "a")
    fetcher.failures = [failure] * 5 + [None, mark_rate_limited(failure), mark_rate_limited(failure)]
    schedule = PollSchedule(timedelta(minutes=1), maximum_backoff=timedelta(minutes=5))
    runner = new_runner(new_poller("first", store, fetcher, RecordingTarget(log)), schedule=schedule)
    await clock.run_until(runner, lambda: log.count("fetch a after 0") == 9)
    minutes = [timedelta(minutes=count) for count in (1, 2, 4, 5, 5, 1, 1, 2)]
    assert clock.waits[1:9] == minutes


@pytest.mark.parametrize("jitter", [0.000001, 0.999999])
async def test_jitter_stays_within_its_bounds(monkeypatch: pytest.MonkeyPatch, jitter: float) -> None:
    clock = FakeClock(monkeypatch, jitter)
    log = OperationLog()
    store = new_memory_cursor_store()
    await seed(store, "0")
    runner = new_runner(
        new_poller("first", store, ChangeLogFetcher(log, "a"), RecordingTarget(log)),
        schedule=PollSchedule(timedelta(seconds=100)),
    )
    await clock.run_until(runner, lambda: log.count("fetch a after 0") == 1)
    assert abs(clock.waits[0].total_seconds() - 10 * jitter) < 0.001
    assert abs(clock.waits[1].total_seconds() - 100 * (0.9 + 0.2 * jitter)) < 0.001


async def test_maximum_pages_per_poll_yields_to_the_next_poller(monkeypatch: pytest.MonkeyPatch) -> None:
    clock = FakeClock(monkeypatch, 0)
    log = OperationLog()
    store_a, store_b = new_memory_cursor_store(), new_memory_cursor_store()
    await seed(store_a, "0")
    await seed(store_b, "0")
    fetcher_a = ChangeLogFetcher(log, "a")
    fetcher_a.append("a1", "a2", "a3")
    runner = new_runner(
        new_poller("first", store_a, fetcher_a, RecordingTarget(log)),
        new_poller("second", store_b, ChangeLogFetcher(log, "b"), RecordingTarget(log)),
        schedule=PollSchedule(timedelta(minutes=1), maximum_pages_per_poll=2),
    )
    await clock.run_until(runner, lambda: log.count("handle a3") == 1)
    assert log.operations == [
        "fetch a after 0", "handle a1", "fetch a after 1", "handle a2",
        "fetch b after 0", "fetch a after 2", "handle a3",
    ]  # fmt: skip


class CancelAfterPrepare(DurableTarget):
    """Cancels the run once an event is recorded, as a crash between record and commit."""

    def __init__(self, log: OperationLog, cancellation: TriggerCancellation) -> None:
        super().__init__(log)
        self.cancellation = cancellation

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        await super().prepare_trigger(context, event)
        self.cancellation.cancel()


@pytest.mark.parametrize("scenario", ["during fetch", "between record and commit"])
async def test_cancellation_before_commit_leaves_the_cursor_unchanged(
    monkeypatch: pytest.MonkeyPatch, scenario: str
) -> None:
    FakeClock(monkeypatch, 0)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    await seed(store, "0")
    fetcher = ChangeLogFetcher(log, "a")
    fetcher.append("a")
    cancellation = TriggerCancellation()
    if scenario == "during fetch":
        fetcher.on_fetch = lambda context: cancellation.cancel()
    runner = new_runner(new_poller("first", store, fetcher, CancelAfterPrepare(log, cancellation)))
    with pytest.raises(TriggerDeliveryAbortedError):
        await runner.run(TriggerContext(cancellation))
    assert log.count("replace 1") == 0
    assert (await store.state()).token == "0"


class SlowFetcher(ChangeLogFetcher):
    """A provider call that never returns on its own."""

    async def fetch_changes(self, context: TriggerContext, cursor: Cursor) -> ChangePage[TriggerEvent[Item]]:
        self.log.add("fetch started")
        await asyncio.Event().wait()
        raise AssertionError("unreachable")


async def test_cancellation_abandons_an_in_flight_provider_call() -> None:
    log = OperationLog()
    store = new_memory_cursor_store()
    await seed(store, "0")
    cancellation = TriggerCancellation()
    runner = new_runner(
        new_poller("first", store, SlowFetcher(log, "a"), RecordingTarget(log)),
        schedule=PollSchedule(timedelta(seconds=1), jitter_fraction=0),
    )
    running = asyncio.ensure_future(runner.run(TriggerContext(cancellation)))
    async with asyncio.timeout(10):
        while log.count("fetch started") == 0:
            await asyncio.sleep(0.01)
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await asyncio.wait_for(running, 10)


class LateFailingFetcher(ChangeLogFetcher):
    """A provider call that outlives its cancellation and then fails."""

    async def fetch_changes(self, context: TriggerContext, cursor: Cursor) -> ChangePage[TriggerEvent[Item]]:
        self.log.add("fetch started")
        try:
            await asyncio.Event().wait()
        except asyncio.CancelledError:
            self.log.add("fetch cancelled")
        raise RuntimeError("late provider failure")


async def test_an_abandoned_provider_call_failing_late_reports_nothing_unretrieved() -> None:
    loop_errors: list[str] = []
    asyncio.get_running_loop().set_exception_handler(
        lambda _loop, failure: loop_errors.append(str(failure.get("message")))
    )
    log = OperationLog()
    store = new_memory_cursor_store()
    await seed(store, "0")
    cancellation = TriggerCancellation()
    runner = new_runner(
        new_poller("first", store, LateFailingFetcher(log, "a"), RecordingTarget(log)),
        schedule=PollSchedule(timedelta(seconds=1), jitter_fraction=0),
    )
    running = asyncio.ensure_future(runner.run(TriggerContext(cancellation)))
    async with asyncio.timeout(10):
        while log.count("fetch started") == 0:
            await asyncio.sleep(0.01)
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running
        while log.count("fetch cancelled") == 0:
            await asyncio.sleep(0.01)
    gc.collect()
    await asyncio.sleep(0)
    assert loop_errors == []


def test_wake_poller_after_the_run_loop_closed_is_ignored() -> None:
    log = OperationLog()
    runner = new_runner(
        new_poller("first", new_memory_cursor_store(), ChangeLogFetcher(log, "a"), RecordingTarget(log)),
        schedule=PollSchedule(timedelta(hours=1), jitter_fraction=0),
    )

    async def run_until_seeded() -> None:
        cancellation = TriggerCancellation()
        running = asyncio.ensure_future(runner.run(TriggerContext(cancellation)))
        async with asyncio.timeout(10):
            while log.count("seed a") == 0:
                await asyncio.sleep(0.01)
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running

    asyncio.run(run_until_seeded())
    failures: list[BaseException] = []

    def wake_from_another_thread() -> None:
        try:
            runner.wake_poller("first")
        except BaseException as error:  # noqa: BLE001
            failures.append(error)

    waker = threading.Thread(target=wake_from_another_thread)
    waker.start()
    waker.join()
    assert failures == []


async def test_run_rejects_an_undecodable_cursor_before_fetching(monkeypatch: pytest.MonkeyPatch) -> None:
    FakeClock(monkeypatch, 0)
    objects = InMemoryObjectStore()
    key = PollCursorKey("changelog", "main", "itemChanged", "first")
    store = PollCursorStore(objects, Scope("p", "live"), key)
    await objects.create_object(store.key, b'{"schemaVersion":"other","state":null}')
    log = OperationLog()
    logger = RecordingLogger()
    runner = new_runner(
        new_poller("first", store, ChangeLogFetcher(log, "a"), RecordingTarget(log)), logger=logger
    )
    with pytest.raises(PollCursorDocumentInvalidError):
        await runner.run()
    assert log.operations == []
    assert [(record[0], record[1]) for record in logger.records] == [
        (TriggerLogLevel.ERROR, "poll cursor document invalid")
    ]
    assert (await objects.read_object(store.key)).contents == b'{"schemaVersion":"other","state":null}'


async def test_poll_records_carry_binding_identity_and_no_payloads(monkeypatch: pytest.MonkeyPatch) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = LoggedCursorStore(new_memory_cursor_store(), log)
    fetcher = ChangeLogFetcher(log, "a")
    fetcher.seed_failures = [RuntimeError("provider unavailable secret-title")]
    fetcher.append("secret-title")
    target = DurableTarget(log, handle_errors={"new": [RuntimeError("Dex is unavailable")]})
    logger = RecordingLogger()
    runner = new_runner(new_poller("first", store, fetcher, target), logger=logger)

    def stop() -> bool:
        if log.count("replace 1") == 1 and log.count("fetch a after 1") == 0 and len(fetcher.changes) == 1:
            fetcher.append("new")
        return log.count("handle new") == 2

    await clock.run_until(runner, stop)
    identity: dict[str, TriggerLogValue] = {
        "connector": "changelog",
        "connection": "main",
        "trigger": "itemChanged",
        "binding": "first",
    }
    expected: list[tuple[TriggerLogLevel, str, dict[str, TriggerLogValue]]] = [
        (
            TriggerLogLevel.WARN,
            "poll failed; retrying",
            {"attempt": 1, "delay_ms": 60_000, "error_type": "RuntimeError"},
        ),
        (TriggerLogLevel.INFO, "poll cursor seeded", {"reset": False}),
        (
            TriggerLogLevel.WARN,
            "trigger delivery failed; retrying",
            {"event_id": "new", "attempt": 1, "delay_ms": 60_000, "error_type": "RuntimeError"},
        ),
        (TriggerLogLevel.INFO, "trigger delivered after retry", {"event_id": "new", "attempts": 2}),
    ]
    for level, message, attributes in expected:
        assert (level, message, {**identity, **attributes}) in logger.records
    assert "secret-title" not in repr(logger.records)


def test_runner_and_pollers_reject_invalid_wiring() -> None:
    log = OperationLog()
    fetcher = ChangeLogFetcher(log, "a")
    target = RecordingTarget(log)
    minute = PollSchedule(timedelta(minutes=1))
    with pytest.raises(ValueError, match="at least one poller"):
        PollRunner(PollRunnerConfig(CONNECTION, minute, []))
    other = TriggerBindingRef(
        ConnectionRef("changelog", "secondary"), definition("itemChanged").trigger, "other"
    )
    other_poller = new_binding_poller(
        BindingPollerConfig(definition("itemChanged"), other, new_memory_cursor_store(), fetcher, target)
    )
    with pytest.raises(ValueError, match="another connection"):
        PollRunner(PollRunnerConfig(CONNECTION, minute, [other_poller]))
    same = [new_poller("same", new_memory_cursor_store(), fetcher, target) for _ in range(2)]
    with pytest.raises(ValueError, match="two pollers"):
        PollRunner(PollRunnerConfig(CONNECTION, minute, same))
    attached = new_poller("attached", new_memory_cursor_store(), fetcher, target)
    new_runner(attached)
    with pytest.raises(ValueError, match="already belongs"):
        new_runner(attached)
    for schedule in [
        PollSchedule(timedelta(0)),
        PollSchedule(timedelta(minutes=1), maximum_backoff=timedelta(seconds=1)),
        PollSchedule(timedelta(minutes=1), maximum_pages_per_poll=-1),
        PollSchedule(timedelta(minutes=1), jitter_fraction=0.6),
    ]:
        with pytest.raises(ValueError):
            new_runner(new_poller("s", new_memory_cursor_store(), fetcher, target), schedule=schedule)
    item_definition, item_binding = definition("itemChanged"), binding("itemChanged", "a")
    for config in [
        BindingPollerConfig(item_definition, item_binding, None, fetcher, target),  # type: ignore[arg-type]
        BindingPollerConfig(item_definition, item_binding, new_memory_cursor_store(), None, target),  # type: ignore[arg-type]
        BindingPollerConfig(item_definition, item_binding, new_memory_cursor_store(), fetcher, None),
        BindingPollerConfig(
            item_definition, binding("otherTrigger", "a"), new_memory_cursor_store(), fetcher, target
        ),
        BindingPollerConfig(
            item_definition,
            item_binding,
            new_memory_cursor_store(),
            fetcher,
            target,
            KeyedSnapshotOptions(200_000),
        ),
    ]:
        with pytest.raises(ValueError):
            new_binding_poller(config)
    member = file_member("created", target)
    for feed_config in [
        FeedPollerConfig("Feed", new_memory_cursor_store(), FileFeedFetcher(log, []), [member]),
        FeedPollerConfig("files", new_memory_cursor_store(), FileFeedFetcher(log, []), []),
        FeedPollerConfig("files", None, FileFeedFetcher(log, []), [member]),  # type: ignore[arg-type]
    ]:
        with pytest.raises(ValueError):
            new_feed_poller(feed_config)
    with pytest.raises(ValueError):
        new_feed_member(definition("fileCreated"), binding("fileCreated", "created"), None, target)  # type: ignore[arg-type]


class CountingLimiter:
    def __init__(self) -> None:
        self.waits = 0
        self.failure: Exception | None = None

    async def wait_for_poll_request(self, context: TriggerContext) -> None:
        self.waits += 1
        if self.failure is not None:
            raise self.failure


async def test_request_limiter_gates_every_provider_request(monkeypatch: pytest.MonkeyPatch) -> None:
    await wait_for_poll_request(TriggerContext())
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    limiter = CountingLimiter()
    runner = new_runner(
        new_poller("first", new_memory_cursor_store(), ChangeLogFetcher(log, "a"), RecordingTarget(log)),
        request_limiter=limiter,
    )

    def stop() -> bool:
        if log.count("fetch a after 0") == 1:
            limiter.failure = RuntimeError("budget exhausted")
        return len(clock.waits) == 4

    await clock.run_until(runner, stop)
    assert limiter.waits == 3
    assert log.operations == ["seed a", "fetch a after 0"]
    assert clock.waits[3] == timedelta(minutes=1)
    cancelled = TriggerCancellation()
    cancelled.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await wait_for_poll_request(TriggerContext(cancelled))


async def test_wake_poller_starts_a_poll_before_its_interval() -> None:
    log = OperationLog()
    runner = new_runner(
        new_poller("first", new_memory_cursor_store(), ChangeLogFetcher(log, "a"), RecordingTarget(log)),
        schedule=PollSchedule(timedelta(hours=1), jitter_fraction=0),
    )
    cancellation = TriggerCancellation()
    running = asyncio.ensure_future(runner.run(TriggerContext(cancellation)))
    async with asyncio.timeout(10):
        while log.count("seed a") == 0:
            await asyncio.sleep(0.01)
        runner.wake_poller("unknown")
        await asyncio.to_thread(runner.wake_poller, "first")
        while log.count("fetch a after 0") == 0:
            await asyncio.sleep(0.01)
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running
    with pytest.raises(RuntimeError, match="already running"):
        await runner.run()


async def test_binding_trigger_source_runs_under_new_trigger(monkeypatch: pytest.MonkeyPatch) -> None:
    clock = FakeClock(monkeypatch, 0.5)
    log = OperationLog()
    store = new_memory_cursor_store()
    await seed(store, "0")
    fetcher = ChangeLogFetcher(log, "a", 10)
    fetcher.append("a")
    config: BindingPollerConfig[Item] = BindingPollerConfig(
        definition("itemChanged"), binding("itemChanged", "main"), store, fetcher
    )
    source = new_binding_trigger_source(config, PollSchedule(timedelta(minutes=1)))
    target = DurableTarget(log)
    trigger = new_trigger(TriggerConfig(config.definition, config.binding, source, target))
    clock.stop_when = lambda: log.count("fetch a after 1") == 1
    with pytest.raises(TriggerDeliveryAbortedError):
        await trigger.run(TriggerContext(TriggerCancellation()))
    assert log.operations == ["replay ", "fetch a after 0", "prepare a", "handle a", "fetch a after 1"]
    with pytest.raises(ValueError):
        new_binding_trigger_source(replace(config, target=target), PollSchedule(timedelta(minutes=1)))


def test_cursor_error_markers() -> None:
    invalid = mark_cursor_invalid(RuntimeError("gone"))
    assert mark_cursor_invalid(invalid) is invalid
    wrapped = RuntimeError("wrapped")
    wrapped.__cause__ = invalid
    assert is_cursor_invalid(wrapped)
    assert not is_cursor_invalid(RuntimeError("other"))
    rate_limited = mark_rate_limited(RuntimeError("429"), timedelta(seconds=-1))
    assert rate_limited.retry_after == timedelta(0)
    assert str(rate_limited) == "provider rate limited the poll: 429"
    assert isinstance(rate_limited, RateLimitedError)


async def test_polling_trigger_example_delivers_only_new_changes(capsys: pytest.CaptureFixture[str]) -> None:
    path = Path(__file__).parents[1] / "examples" / "polling_trigger.py"
    specification = importlib.util.spec_from_file_location("polling_trigger_example", path)
    assert specification is not None and specification.loader is not None
    example = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(example)
    await example.main()
    assert capsys.readouterr().out == "delivered item-1@2 item-2@3\n"
