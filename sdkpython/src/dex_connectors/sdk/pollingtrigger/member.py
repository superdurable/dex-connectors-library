# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Trigger binding's delivery state inside a poller: staging, recording, and in-order delivery."""

from collections.abc import Callable, Sequence
from datetime import timedelta
from enum import Enum
from typing import Any, Generic, Protocol, TypeVar

from ..projectconfig.poll_cursor import KeyedSnapshotEntry
from ..trigger import TriggerBindingRef, TriggerDefinition
from ..trigger_delivery import (
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerDeliveryPreparer,
    TriggerDeliveryReplayer,
    TriggerEvent,
    TriggerLogger,
    TriggerLogLevel,
    TriggerLogValue,
    TriggerTarget,
    bind_trigger_log_attributes,
    is_trigger_undeliverable,
    log_trigger_record,
    prepare_trigger_delivery,
    start_trigger_attempt,
    trigger_error_attributes,
)

ChangeT = TypeVar("ChangeT")
P = TypeVar("P")

FeedEventSelector = Callable[[ChangeT, KeyedSnapshotEntry | None], Sequence[TriggerEvent[P]]]
"""Maps one provider change and its previous snapshot entry to zero or more of a member's events.

The entry is the change's entry in the feed snapshot committed before this page, or None when the change
does not implement KeyedItem, the feed keeps no snapshot, or the key is untracked. A selector must be
deterministic and side-effect free: a page is selected again when it is refetched after a failure.
"""


class _DeliveryMode(Enum):
    # Records in a durable target, commits, then delivers.
    RECORD_BEFORE_COMMIT = "record_before_commit"
    # Consumes in a non-durable target before the commit.
    DELIVER_BEFORE_COMMIT = "deliver_before_commit"
    # Prepares and consumes before the commit, for a target whose durability new_trigger hides.
    RECORD_AND_DELIVER_BEFORE_COMMIT = "record_and_deliver_before_commit"


class _FailureDelaySource(Protocol):
    def failure_delay(self) -> timedelta: ...


class _RecordFailedError(Exception):
    """A durable target failed to record an event before the commit."""

    def __init__(self, cause: BaseException) -> None:
        super().__init__(f"record Trigger event: {cause}")
        self.__cause__ = cause


class _DeliveryFailedError(Exception):
    """A target failure that the member already logged."""

    def __init__(self, cause: BaseException) -> None:
        super().__init__(f"deliver Trigger event: {cause}")
        self.__cause__ = cause


def _binding_attributes(binding: TriggerBindingRef) -> dict[str, TriggerLogValue]:
    return {
        "connector": binding.trigger.connector_id,
        "connection": binding.connection.name,
        "trigger": binding.trigger.trigger_name,
        "binding": binding.name,
    }


def _validate_member_binding(definition: TriggerDefinition, binding: TriggerBindingRef) -> None:
    try:
        definition.validate()
    except ValueError as error:
        raise ValueError(f"poll trigger definition: {error}") from error
    try:
        binding.validate()
    except ValueError as error:
        raise ValueError(f"poll trigger binding: {error}") from error
    if binding.trigger != definition.trigger:
        raise ValueError("poll trigger binding does not match its definition")


def _delivery_mode_for(target: object) -> _DeliveryMode:
    if isinstance(target, TriggerDeliveryPreparer):
        return _DeliveryMode.RECORD_BEFORE_COMMIT
    return _DeliveryMode.DELIVER_BEFORE_COMMIT


class FeedMember(Generic[ChangeT]):
    """One Trigger binding's view of a shared provider change feed; build it with `new_feed_member`.

    A member belongs to one poller. Its methods other than `binding` are internal to the kit.
    """

    def __init__(
        self,
        definition: TriggerDefinition,
        binding: TriggerBindingRef,
        select_events: FeedEventSelector[ChangeT, Any],
        target: TriggerTarget[Any],
        mode: _DeliveryMode,
        should_replay: bool,
    ) -> None:
        """Bind validated wiring; applications call `new_feed_member` instead."""
        self._definition = definition
        self._binding = binding
        self._select_events = select_events
        self._target = target
        self._mode = mode
        self._should_replay = should_replay
        self._logger = bind_trigger_log_attributes(None, _binding_attributes(binding))
        self._staged: list[TriggerEvent[Any]] = []
        # Events recorded or consumed at processed_revision; a retry skips their IDs even when the
        # provider now returns a different page.
        self._processed_revision = ""
        self._processed_ids: set[str] = set()
        self._processed: list[TriggerEvent[Any]] = []
        # Recorded events whose cursor is committed but that the target has not consumed.
        self._queue: list[TriggerEvent[Any]] = []
        self._retry_event_id = ""
        self._retry_failures = 0

    @property
    def binding(self) -> TriggerBindingRef:
        """The Trigger binding this member delivers to."""
        return self._binding

    def _attach_logger(self, logger: TriggerLogger | None) -> None:
        self._logger = bind_trigger_log_attributes(logger, _binding_attributes(self._binding))

    async def _replay(self, context: TriggerContext) -> None:
        if not self._should_replay or not isinstance(self._target, TriggerDeliveryReplayer):
            return
        try:
            await self._target.replay_trigger_deliveries(context)
        except Exception as error:
            raise RuntimeError(
                f"replay Trigger deliveries of binding {self._binding.name!r}: {error}"
            ) from error

    def _stage(
        self,
        context: TriggerContext,
        changes: Sequence[ChangeT],
        previous_entry: Callable[[ChangeT], KeyedSnapshotEntry | None],
        revision: str,
    ) -> None:
        """Select this member's events from a page fetched at the cursor revision."""
        self._staged = []
        for change in changes:
            for event in self._select_events(change, previous_entry(change)):
                if not event.id.strip():
                    log_trigger_record(
                        self._logger,
                        context,
                        TriggerLogLevel.WARN,
                        "trigger event skipped: undeliverable",
                        {
                            "event_id": event.id,
                            **trigger_error_attributes(ValueError("Trigger event ID is required")),
                        },
                    )
                    continue
                self._staged.append(event)
        if self._processed_revision != revision:
            # The cursor moved without this poller's commit, such as after an unknown write outcome.
            self._settle_processed()
            self._processed_revision = revision

    async def _record_staged(self, context: TriggerContext, turn: _FailureDelaySource) -> None:
        """Record or consume staged events not yet processed at this revision, stopping at a failure."""
        for event in self._staged:
            if event.id in self._processed_ids:
                continue
            context.cancellation.raise_if_cancelled()
            if self._mode is not _DeliveryMode.DELIVER_BEFORE_COMMIT:
                try:
                    await prepare_trigger_delivery(context, self._target, event)
                except Exception as error:
                    raise _RecordFailedError(error) from error
            if self._mode is not _DeliveryMode.RECORD_BEFORE_COMMIT:
                await self._deliver(context, turn, event)
            self._processed_ids.add(event.id)
            self._processed.append(event)

    def _accept_staged(self) -> None:
        """Queue recorded events once the cursor moved past them, by this poller's commit or a replica's."""
        self._settle_processed()
        self._staged = []
        self._processed_revision = ""

    def _settle_processed(self) -> None:
        """Queue the events recorded at the processed revision, even ones a refetch dropped."""
        if self._mode is _DeliveryMode.RECORD_BEFORE_COMMIT:
            self._queue.extend(self._processed)
        self._processed = []
        self._processed_ids = set()

    def _staged_count(self) -> int:
        return len(self._staged)

    async def _drain(self, context: TriggerContext, turn: _FailureDelaySource) -> None:
        """Deliver queued events in order, stopping at the first retryable failure."""
        while self._queue:
            await self._deliver(context, turn, self._queue[0])
            self._queue.pop(0)

    async def _deliver(
        self, context: TriggerContext, turn: _FailureDelaySource, event: TriggerEvent[Any]
    ) -> None:
        """Make one attempt with `deliver_trigger`'s records; a failure is retried on a later poll."""
        # A Go context ends in-flight attempts; a cancelled run must not start one.
        context.cancellation.raise_if_cancelled()
        failures = self._retry_failures if self._retry_event_id == event.id else 0
        attempt = start_trigger_attempt(context)
        try:
            await self._target.handle_trigger(attempt.context, event)
        except Exception as error:
            if is_trigger_undeliverable(error):
                log_trigger_record(
                    self._logger,
                    context,
                    TriggerLogLevel.WARN,
                    "trigger event skipped: undeliverable",
                    {"event_id": event.id, **trigger_error_attributes(error)},
                )
            elif context.cancellation.is_cancelled:
                raise TriggerDeliveryAbortedError() from error
            else:
                self._retry_event_id, self._retry_failures = event.id, failures + 1
                log_trigger_record(
                    self._logger,
                    context,
                    TriggerLogLevel.WARN,
                    "trigger delivery failed; retrying",
                    {
                        "event_id": event.id,
                        "attempt": failures + 1,
                        "delay_ms": turn.failure_delay() // timedelta(milliseconds=1),
                        **trigger_error_attributes(error),
                    },
                )
                raise _DeliveryFailedError(error) from error
        else:
            if failures > 0 and not attempt.was_skipped():
                log_trigger_record(
                    self._logger,
                    context,
                    TriggerLogLevel.INFO,
                    "trigger delivered after retry",
                    {"event_id": event.id, "attempts": failures + 1},
                )
        self._retry_event_id, self._retry_failures = "", 0


def new_feed_member(
    definition: TriggerDefinition,
    binding: TriggerBindingRef,
    select_events: FeedEventSelector[ChangeT, P],
    target: TriggerTarget[P],
) -> FeedMember[ChangeT]:
    """Bind one Trigger binding to a feed with its selector and target.

    A target that implements TriggerDeliveryPreparer, such as `new_durable_trigger_target`, records
    events before the feed cursor commits and receives them after; any other target consumes them before
    the commit. A target that implements TriggerDeliveryReplayer is replayed when the runner starts.
    Raises ValueError for invalid wiring.
    """
    _validate_member_binding(definition, binding)
    if select_events is None or target is None:
        raise ValueError("poll feed member selector and target are required")
    return FeedMember(definition, binding, select_events, target, _delivery_mode_for(target), True)
