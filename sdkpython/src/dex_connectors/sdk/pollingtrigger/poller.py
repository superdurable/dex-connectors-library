# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Pollers: one durable cursor, its change fetcher, and the Trigger bindings it serves."""

import asyncio
import re
from collections.abc import Awaitable, Sequence
from dataclasses import dataclass, replace
from datetime import timedelta
from enum import Enum
from typing import Any, Generic, TypeVar

from ..projectconfig.poll_cursor import (
    KeyedSnapshot,
    KeyedSnapshotEntry,
    PollCursorDocumentInvalidError,
    PollCursorRecord,
    PollCursorState,
    PollCursorTooLargeError,
    validate_poll_cursor_state,
)
from ..projectconfig.storage import ObjectConflictError
from ..projectconfig.trigger_inbox import TriggerInboxFullError
from ..trigger import TriggerBindingRef, TriggerDefinition
from ..trigger_delivery import (
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerLogger,
    TriggerLogLevel,
    TriggerLogValue,
    TriggerTarget,
    bind_trigger_log_attributes,
    log_trigger_record,
    trigger_error_attributes,
)
from . import schedule
from .cursor_store import CursorStore
from .fetcher import (
    MAXIMUM_CHANGES_PER_PAGE,
    ChangeFetcher,
    ChangePage,
    Cursor,
    RateLimitedError,
    _find_in_causes,
    is_cursor_invalid,
)
from .member import (
    FeedMember,
    _binding_attributes,
    _delivery_mode_for,
    _DeliveryFailedError,
    _DeliveryMode,
    _FailureDelaySource,
    _validate_member_binding,
)
from .snapshot import (
    KeyedItem,
    KeyedSnapshotOptions,
    _lookup_keyed_snapshot_entry,
    _resolve_keyed_snapshot_options,
)

ChangeT = TypeVar("ChangeT")
P = TypeVar("P")

_FEED_NAME = re.compile(r"[a-z][A-Za-z0-9]{1,62}", re.ASCII)
_CURSOR_BOUND_MESSAGE = "poll paused: cursor state exceeds bound"
_RETRYING_MESSAGE = "poll failed; retrying"


class _PollOutcomeKind(Enum):
    SUCCEEDED = "succeeded"
    HAS_MORE_PAGES = "has_more_pages"
    FAILED = "failed"
    RATE_LIMITED = "rate_limited"
    CONFIGURATION_FAULT = "configuration_fault"
    CANCELED = "canceled"


@dataclass(frozen=True, slots=True)
class _PollOutcome:
    kind: _PollOutcomeKind
    error: BaseException | None = None
    # The WARN or ERROR record the runner writes; empty when the poll already logged it.
    message: str = ""
    retry_after: timedelta = timedelta(0)


_SUCCEEDED = _PollOutcome(_PollOutcomeKind.SUCCEEDED)
_CANCELED = _PollOutcome(_PollOutcomeKind.CANCELED)


@dataclass(frozen=True, slots=True)
class BindingPollerConfig(Generic[P]):
    """Configures a poller that owns one Trigger binding and its cursor."""

    definition: TriggerDefinition
    """The immutable Trigger definition; `binding.trigger` must equal its Trigger."""
    binding: TriggerBindingRef
    """The configured Trigger binding; its name is the poller name for `PollRunner.wake_poller`."""
    cursor: CursorStore
    """Persists the binding's position, normally `LoadedProject.poll_cursor_store`."""
    fetcher: ChangeFetcher[TriggerEvent[P]]
    """Reads the provider; its changes are this binding's events."""
    target: TriggerTarget[P] | None = None
    """Receives events; required by `new_binding_poller` and None for `new_binding_trigger_source`.

    Wrap it with `new_durable_trigger_target` so events are recorded in the project Trigger inbox before
    the cursor commits; a non-durable target consumes each event first.
    """
    snapshot: KeyedSnapshotOptions | None = None
    """Enables a keyed snapshot that the fetcher keeps in `Cursor.snapshot`; None disables it."""


@dataclass(frozen=True, slots=True)
class FeedPollerConfig(Generic[ChangeT]):
    """Configures one provider change feed shared by several Trigger bindings of one connection."""

    name: str
    """The feed's storage identity (`PollFeedKey.feed_name`) and poller name for `wake_poller`.

    Lower camel case, 2 to 63 characters, and stable forever.
    """
    cursor: CursorStore
    """Persists the feed position, normally `LoadedProject.poll_feed_cursor_store`."""
    fetcher: ChangeFetcher[ChangeT]
    """Reads the provider feed once per poll for every member."""
    members: Sequence[FeedMember[ChangeT]]
    """Recorded and delivered in this order for every page; one connector and connection, at least one."""
    snapshot: KeyedSnapshotOptions | None = None
    """Enables a keyed snapshot; when changes implement KeyedItem each selector gets the change's entry."""


async def _await_unless_cancelled(context: TriggerContext, work: Awaitable[Any]) -> Any:
    """Await a provider call, abandoning it as soon as the context is cancelled."""
    task = asyncio.ensure_future(work)
    cancelled = asyncio.ensure_future(context.cancellation.wait())
    try:
        await asyncio.wait({task, cancelled}, return_when=asyncio.FIRST_COMPLETED)
    finally:
        cancelled.cancel()
        if not task.done():
            task.cancel()
            # Nobody awaits an abandoned call, so its late outcome is retrieved here instead.
            task.add_done_callback(_retrieve_abandoned_outcome)
    if not task.done() or task.cancelled():
        raise TriggerDeliveryAbortedError()
    return task.result()


def _retrieve_abandoned_outcome(task: "asyncio.Future[Any]") -> None:
    # A late failure of an abandoned provider call is deliberately dropped: the poll already ended.
    if not task.cancelled():
        task.exception()


class Poller(Generic[ChangeT]):
    """One durable cursor, its change fetcher, and the bindings it serves.

    Build it with `new_binding_poller` or `new_feed_poller` and pass it to exactly one PollRunner. Its
    methods other than `name` and `bindings` are internal to the kit.
    """

    def __init__(
        self,
        name: str,
        identity: dict[str, TriggerLogValue],
        cursor: CursorStore,
        fetcher: ChangeFetcher[ChangeT],
        members: Sequence[FeedMember[ChangeT]],
        snapshot: KeyedSnapshotOptions | None,
    ) -> None:
        """Bind validated wiring; applications call `new_binding_poller` or `new_feed_poller` instead."""
        self._name = name
        self._identity = identity
        self._cursor = cursor
        self._fetcher = fetcher
        self._members = tuple(members)
        self._snapshot = snapshot
        self._logger = bind_trigger_log_attributes(None, identity)
        self._is_attached = False
        self._pages_per_poll = 1

    @property
    def name(self) -> str:
        """The binding or feed name that `PollRunner.wake_poller` takes."""
        return self._name

    @property
    def bindings(self) -> tuple[TriggerBindingRef, ...]:
        """The Trigger bindings this poller serves, in delivery order."""
        return tuple(member.binding for member in self._members)

    def _attach_runner(self, logger: TriggerLogger | None, pages_per_poll: int) -> None:
        if self._is_attached:
            raise ValueError(f"poller {self._name!r} already belongs to a runner")
        self._is_attached, self._pages_per_poll = True, pages_per_poll
        self._logger = bind_trigger_log_attributes(logger, self._identity)
        for member in self._members:
            member._attach_logger(logger)

    def _log_record(
        self,
        context: TriggerContext,
        level: TriggerLogLevel,
        message: str,
        attributes: dict[str, TriggerLogValue],
    ) -> None:
        log_trigger_record(self._logger, context, level, message, attributes)

    async def _check_stored_cursor(self, context: TriggerContext) -> None:
        """Raise for a stored document that can never be decoded; other errors wait for a poll."""
        try:
            await self._cursor.read_poll_cursor()
        except PollCursorDocumentInvalidError as error:
            self._log_record(
                context,
                TriggerLogLevel.ERROR,
                "poll cursor document invalid",
                trigger_error_attributes(error),
            )
            raise
        except Exception:  # noqa: BLE001
            # A transient storage failure is retried by the first poll.
            return

    async def _replay(self, context: TriggerContext) -> None:
        for member in self._members:
            await member._replay(context)

    async def _poll(self, context: TriggerContext, turn: _FailureDelaySource) -> _PollOutcome:
        """Deliver earlier recorded events, then read pages until current, out of budget, or failed.

        A commit moves past a page only after every member recorded or consumed it.
        """
        outcome = await self._drain_members(context, turn)
        if outcome is not None:
            return outcome
        try:
            record = await self._cursor.read_poll_cursor()
        except Exception as error:
            return self._store_failure(context, error)
        if record.state is None:
            return await self._seed(context, record, None)
        pages = 1
        while True:
            state = _committed_state(record)
            try:
                page: ChangePage[ChangeT] = await _await_unless_cancelled(
                    context, self._fetcher.fetch_changes(context, Cursor(state.token, state.snapshot))
                )
            except Exception as error:
                if is_cursor_invalid(error) and not context.cancellation.is_cancelled:
                    return await self._seed(context, record, error)
                return self._provider_failure(context, error)
            fault = self._validate_page(page)
            if fault is not None:
                return _PollOutcome(_PollOutcomeKind.CONFIGURATION_FAULT, fault, _CURSOR_BOUND_MESSAGE)
            next_record, outcome = await self._record_and_commit(context, turn, record, page)
            if outcome is not None:
                return outcome
            outcome = await self._drain_members(context, turn)
            if outcome is not None:
                return outcome
            if next_record is None or not page.has_more_pages:
                return _SUCCEEDED
            if pages >= self._pages_per_poll:
                return _PollOutcome(_PollOutcomeKind.HAS_MORE_PAGES)
            record = next_record
            pages += 1

    async def _record_and_commit(
        self,
        context: TriggerContext,
        turn: _FailureDelaySource,
        record: PollCursorRecord,
        page: ChangePage[ChangeT],
    ) -> tuple[PollCursorRecord | None, _PollOutcome | None]:
        """Record one page in every member and commit its next cursor.

        Returns the new record, or None after a commit conflict, when another replica owns the position.
        """
        state = _committed_state(record)

        def previous_entry(change: ChangeT) -> KeyedSnapshotEntry | None:
            if state.snapshot is None or not isinstance(change, KeyedItem):
                return None
            return _lookup_keyed_snapshot_entry(state.snapshot, change.snapshot_key())

        events = 0
        for member in self._members:
            member._stage(context, page.changes, previous_entry, record.revision)
            try:
                await member._record_staged(context, turn)
            except Exception as error:
                return None, self._record_failure(context, error)
            events += member._staged_count()
        if context.cancellation.is_cancelled:
            return None, _CANCELED
        if events == 0 and not page.changes and _is_same_cursor(state, page.next_cursor):
            # An empty page that does not move the cursor needs no write. Events an earlier attempt
            # recorded at this position are owed a delivery.
            self._accept_staged()
            return record, None
        commit_count = state.commit_count + 1
        committed = replace(
            state,
            token=page.next_cursor.token,
            snapshot=_stamp_snapshot_seen(page.next_cursor.snapshot, commit_count),
            commit_count=commit_count,
            committed_at_unix_ms=schedule._unix_milliseconds(schedule._now()),
        )
        try:
            written = await self._cursor.replace_poll_cursor(record.revision, committed)
        except ObjectConflictError:
            self._log_record(context, TriggerLogLevel.DEBUG, "poll cursor commit conflict", {})
            self._accept_staged()
            return None, None
        except Exception as error:
            return None, self._store_failure(context, error)
        self._log_record(
            context,
            TriggerLogLevel.DEBUG,
            "poll cursor committed",
            {"commit_count": commit_count, "events": events},
        )
        self._accept_staged()
        return written, None

    def _accept_staged(self) -> None:
        for member in self._members:
            member._accept_staged()

    async def _drain_members(self, context: TriggerContext, turn: _FailureDelaySource) -> _PollOutcome | None:
        for member in self._members:
            try:
                await member._drain(context, turn)
            except Exception as error:
                if context.cancellation.is_cancelled:
                    return _CANCELED
                return _PollOutcome(_PollOutcomeKind.FAILED, error)
        return None

    async def _seed(
        self, context: TriggerContext, record: PollCursorRecord, invalid_cursor: BaseException | None
    ) -> _PollOutcome:
        """Commit the provider's current position: first when invalid_cursor is None, else as a reset.

        Changes in a reset's gap are not delivered.
        """
        state = record.state if record.state is not None else PollCursorState()
        is_reset = invalid_cursor is not None
        if invalid_cursor is not None:
            self._log_record(
                context,
                TriggerLogLevel.WARN,
                "poll cursor invalid; reseeding",
                {"reset_count": state.reset_count + 1, **trigger_error_attributes(invalid_cursor)},
            )
        try:
            cursor: Cursor = await _await_unless_cancelled(context, self._fetcher.seed_cursor(context))
        except Exception as error:
            return self._provider_failure(context, error)
        fault = self._validate_cursor(cursor)
        if fault is not None:
            return _PollOutcome(_PollOutcomeKind.CONFIGURATION_FAULT, fault, _CURSOR_BOUND_MESSAGE)
        if context.cancellation.is_cancelled:
            return _CANCELED
        committed_at = schedule._unix_milliseconds(schedule._now())
        commit_count = state.commit_count + 1
        seeded = replace(
            state,
            token=cursor.token,
            snapshot=_stamp_snapshot_seen(cursor.snapshot, commit_count),
            seeded_at_unix_ms=committed_at,
            committed_at_unix_ms=committed_at,
            commit_count=commit_count,
        )
        if is_reset:
            seeded = replace(seeded, reset_count=state.reset_count + 1, last_reset_at_unix_ms=committed_at)
        try:
            await self._cursor.replace_poll_cursor(record.revision, seeded)
        except ObjectConflictError:
            self._log_record(context, TriggerLogLevel.DEBUG, "poll cursor commit conflict", {})
            return _SUCCEEDED
        except Exception as error:
            return self._store_failure(context, error)
        if not is_reset:
            self._log_record(context, TriggerLogLevel.INFO, "poll cursor seeded", {"reset": False})
        return _SUCCEEDED

    def _provider_failure(self, context: TriggerContext, error: BaseException) -> _PollOutcome:
        if context.cancellation.is_cancelled:
            return _CANCELED
        rate_limited = _find_in_causes(error, RateLimitedError)
        if isinstance(rate_limited, RateLimitedError):
            return _PollOutcome(_PollOutcomeKind.RATE_LIMITED, error, retry_after=rate_limited.retry_after)
        return _PollOutcome(_PollOutcomeKind.FAILED, error, _RETRYING_MESSAGE)

    def _store_failure(self, context: TriggerContext, error: BaseException) -> _PollOutcome:
        if context.cancellation.is_cancelled:
            return _CANCELED
        if isinstance(error, PollCursorTooLargeError):
            return _PollOutcome(_PollOutcomeKind.CONFIGURATION_FAULT, error, _CURSOR_BOUND_MESSAGE)
        if isinstance(error, PollCursorDocumentInvalidError):
            return _PollOutcome(_PollOutcomeKind.CONFIGURATION_FAULT, error, "poll cursor document invalid")
        return _PollOutcome(_PollOutcomeKind.FAILED, error, _RETRYING_MESSAGE)

    def _record_failure(self, context: TriggerContext, error: BaseException) -> _PollOutcome:
        if context.cancellation.is_cancelled:
            return _CANCELED
        if _find_in_causes(error, _DeliveryFailedError) is not None:
            return _PollOutcome(_PollOutcomeKind.FAILED, error)
        if _find_in_causes(error, TriggerInboxFullError) is not None:
            return _PollOutcome(_PollOutcomeKind.FAILED, error, "poll paused: trigger inbox full")
        return _PollOutcome(_PollOutcomeKind.FAILED, error, _RETRYING_MESSAGE)

    def _validate_page(self, page: ChangePage[ChangeT]) -> BaseException | None:
        """Reject fetcher output that no retry can store."""
        if len(page.changes) > MAXIMUM_CHANGES_PER_PAGE:
            return PollCursorTooLargeError(
                f"project poll cursor state exceeds its bound: page has {len(page.changes)} changes, "
                f"more than {MAXIMUM_CHANGES_PER_PAGE}"
            )
        return self._validate_cursor(page.next_cursor)

    def _validate_cursor(self, cursor: Cursor) -> BaseException | None:
        if cursor.snapshot is not None:
            if self._snapshot is None:
                return ValueError(
                    "fetcher returned a keyed snapshot but the poller has no KeyedSnapshotOptions"
                )
            if len(cursor.snapshot.entries) > self._snapshot.max_entries:
                return PollCursorTooLargeError(
                    "project poll cursor state exceeds its bound: snapshot has "
                    f"{len(cursor.snapshot.entries)} entries, more than {self._snapshot.max_entries}"
                )
        try:
            validate_poll_cursor_state(PollCursorState(cursor.token, cursor.snapshot))
        except ValueError as error:
            return error
        return None


def _committed_state(record: PollCursorRecord) -> PollCursorState:
    if record.state is None:
        raise RuntimeError("poll cursor record must be seeded")
    return record.state


def _stamp_snapshot_seen(snapshot: KeyedSnapshot | None, commit_count: int) -> KeyedSnapshot | None:
    """Replace `seen` zero, meaning "this commit", with the commit's commit count."""
    if snapshot is None:
        return None
    return KeyedSnapshot(
        tuple(entry if entry.seen else replace(entry, seen=commit_count) for entry in snapshot.entries)
    )


def _is_same_cursor(state: PollCursorState, cursor: Cursor) -> bool:
    """Report whether cursor equals the committed position, entry by entry."""
    if state.token != cursor.token or (state.snapshot is None) != (cursor.snapshot is None):
        return False
    if state.snapshot is None or cursor.snapshot is None:
        return True
    if len(state.snapshot.entries) != len(cursor.snapshot.entries):
        return False
    return all(
        stored.key == current.key
        and stored.hash == current.hash
        and (current.seen == 0 or current.seen == stored.seen)
        and stored.value == current.value
        for stored, current in zip(state.snapshot.entries, cursor.snapshot.entries, strict=True)
    )


def _resolve_poller_snapshot(options: KeyedSnapshotOptions | None) -> KeyedSnapshotOptions | None:
    return None if options is None else _resolve_keyed_snapshot_options(options)


def _select_binding_event(
    change: TriggerEvent[Any], previous: KeyedSnapshotEntry | None
) -> list[TriggerEvent[Any]]:
    return [change]


def _new_binding_poller(
    config: BindingPollerConfig[P], target: TriggerTarget[P], mode: _DeliveryMode, should_replay: bool
) -> Poller[TriggerEvent[P]]:
    _validate_member_binding(config.definition, config.binding)
    if config.cursor is None or config.fetcher is None:
        raise ValueError("poll Trigger cursor store and fetcher are required")
    snapshot = _resolve_poller_snapshot(config.snapshot)
    member: FeedMember[TriggerEvent[P]] = FeedMember(
        config.definition, config.binding, _select_binding_event, target, mode, should_replay
    )
    return Poller(
        config.binding.name,
        _binding_attributes(config.binding),
        config.cursor,
        config.fetcher,
        [member],
        snapshot,
    )


def new_binding_poller(config: BindingPollerConfig[P]) -> Poller[TriggerEvent[P]]:
    """Validate config and return a Poller for one binding; it reads and writes nothing.

    Raises ValueError when the target, cursor, or fetcher is missing, the binding does not match the
    definition, or the snapshot options are out of range.
    """
    if config.target is None:
        raise ValueError("poll Trigger target is required")
    return _new_binding_poller(config, config.target, _delivery_mode_for(config.target), True)


def new_feed_poller(config: FeedPollerConfig[ChangeT]) -> Poller[ChangeT]:
    """Validate config and return a Poller for one shared feed; it reads and writes nothing.

    Raises ValueError for an invalid feed name, a missing cursor, fetcher, or member, members of
    different connectors or connections, or snapshot options out of range.
    """
    if not _FEED_NAME.fullmatch(config.name):
        raise ValueError("poll feed name must be lower camel case, 2 to 63 characters")
    if config.cursor is None or config.fetcher is None or not config.members:
        raise ValueError("poll feed cursor store, fetcher, and members are required")
    snapshot = _resolve_poller_snapshot(config.snapshot)
    if any(member is None for member in config.members):
        raise ValueError("poll feed member is None")
    first = config.members[0].binding
    for member in config.members:
        if (
            member.binding.trigger.connector_id != first.trigger.connector_id
            or member.binding.connection != first.connection
        ):
            raise ValueError(f"poll feed {config.name!r} members must share one connector and connection")
    identity: dict[str, TriggerLogValue] = {
        "connector": first.trigger.connector_id,
        "connection": first.connection.name,
        "feed": config.name,
    }
    return Poller(config.name, identity, config.cursor, config.fetcher, config.members, snapshot)
