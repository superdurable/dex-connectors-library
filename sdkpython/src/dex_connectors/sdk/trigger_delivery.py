# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Trigger delivery outcomes, contexts, records, and the retrying delivery loop.

Type aliases:

| Name | Meaning |
|---|---|
| `TriggerLogValue` | One safe record attribute value: an ID, count, enum value, or flag. |
"""

import asyncio
import json
import logging
import re
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from enum import Enum, StrEnum
from types import MappingProxyType
from typing import Any, Final, Generic, Protocol, TypeVar, runtime_checkable

PayloadT = TypeVar("PayloadT")
PayloadT_contra = TypeVar("PayloadT_contra", contravariant=True)
PayloadT_co = TypeVar("PayloadT_co", covariant=True)

UNDELIVERABLE_TRIGGER_MESSAGE: Final = "Trigger event is undeliverable"
"""Prefix of every UndeliverableTriggerError message; RPC targets recognize a handler's marker by it."""

UNDELIVERABLE_TRIGGER_ERROR_TYPE: Final = "ConnectorsSDK.UndeliverableTriggerError"
"""Error type a Dex Worker reports for an RPC handler's UndeliverableTriggerError, shared with TypeScript."""

_MAXIMUM_CAUSE_DEPTH = 32
_ERROR_TYPE_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_]{0,63}", re.ASCII)
_RETRY_INITIAL_DELAY = timedelta(milliseconds=250)
_RETRY_MAXIMUM_DELAY = timedelta(seconds=30)


def _restore_undeliverable_trigger_error(cause: str | None) -> "UndeliverableTriggerError":
    return UndeliverableTriggerError(cause)


class UndeliverableTriggerError(Exception):
    """Marks an event no retry can deliver; the source consumes it.

    Durable inboxes, Trigger runners, and `deliver_trigger` consume the event instead of retrying it. A
    Dex RPC handler raises this exact class, never a subclass or a wrapper, to reject an event: its type
    is pinned to `ConnectorsSDK.UndeliverableTriggerError`, which Python and TypeScript RPC targets
    recognize. Go RPC targets do not recognize it, and Python targets do not recognize Go markers, so a
    polyglot handler returns a normal success for an event it rejects.

    Instances pickle with their cause reduced to text; the class itself cannot be pickled by reference.
    """

    __module__ = "ConnectorsSDK"
    __qualname__ = "UndeliverableTriggerError"

    def __init__(self, cause: BaseException | str | None = None) -> None:
        """Wrap the reason; the message is the marker followed by the reason's text when it has any."""
        reason = "" if cause is None else str(cause)
        super().__init__(
            f"{UNDELIVERABLE_TRIGGER_MESSAGE}: {reason}" if reason else UNDELIVERABLE_TRIGGER_MESSAGE
        )
        self.cause = cause
        """The reason this event is undeliverable, if one was given."""
        if isinstance(cause, BaseException):
            self.__cause__ = cause

    def __reduce__(self) -> tuple[Any, ...]:
        """Pickle the cause as text, so arbitrary causes never need to pickle."""
        return (_restore_undeliverable_trigger_error, (None if self.cause is None else str(self.cause),))


def mark_trigger_undeliverable(error: BaseException) -> UndeliverableTriggerError:
    """Return error when it is already the marker, otherwise a new marker caused by error."""
    return error if isinstance(error, UndeliverableTriggerError) else UndeliverableTriggerError(error)


def is_trigger_undeliverable(error: BaseException | None) -> bool:
    """Report whether error or an error in its `__cause__` chain, up to 32 links, is the marker.

    `__context__` and ExceptionGroup members are not searched.
    """
    current = error
    for _ in range(_MAXIMUM_CAUSE_DEPTH):
        if current is None:
            return False
        if isinstance(current, UndeliverableTriggerError):
            return True
        current = current.__cause__
    return False


class TriggerDeliveryAbortedError(Exception):
    """Raised when a Trigger context's cancellation ends delivery."""

    def __init__(self) -> None:
        """Create the abort error with its fixed message."""
        super().__init__("Trigger delivery was aborted")


class TriggerCancellation:
    """Cooperative cancellation shared by a Trigger run or one webhook arrival slot.

    Cancelling stops new work, such as a retry wait or a late inbox write, without interrupting work
    already in flight. A child is cancelled when its parent is. The object is bound to the event loop
    of its first `wait()`.
    """

    def __init__(self, parent: "TriggerCancellation | None" = None) -> None:
        """Create a cancellation, already cancelled when parent is."""
        self._is_cancelled = False
        self._event: asyncio.Event | None = None
        self._children: list[TriggerCancellation] = []
        if parent is not None:
            if parent.is_cancelled:
                self._is_cancelled = True
            else:
                parent._children.append(self)

    def cancel(self) -> None:
        """Cancel this and every child; calling it again has no effect."""
        if self._is_cancelled:
            return
        self._is_cancelled = True
        if self._event is not None:
            self._event.set()
        children, self._children = self._children, []
        for child in children:
            child.cancel()

    @property
    def is_cancelled(self) -> bool:
        """Report whether cancellation was requested."""
        return self._is_cancelled

    async def wait(self) -> None:
        """Return once cancellation was requested."""
        if self._is_cancelled:
            return
        if self._event is None:
            self._event = asyncio.Event()
        await self._event.wait()

    def raise_if_cancelled(self) -> None:
        """Raise TriggerDeliveryAbortedError when cancellation was requested."""
        if self._is_cancelled:
            raise TriggerDeliveryAbortedError()


TriggerLogValue = str | int | float | bool
"""One safe record attribute value: an ID, count, enum value, or flag, never a payload or message."""


class TriggerLogLevel(StrEnum):
    """Severity of one Trigger delivery record."""

    DEBUG = "debug"
    """Per-event delivery detail."""
    INFO = "info"
    """Replay progress, filtered events, and recovered deliveries."""
    WARN = "warn"
    """Retried failures and undeliverable events."""
    ERROR = "error"
    """Inbox read, write, and removal failures."""


class TriggerLogger(Protocol):
    """Receives Trigger delivery records. Implementations must not raise."""

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        """Report whether records at level are written, so callers can skip building them."""
        ...

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        """Write one record."""
        ...


_STANDARD_LEVELS: Final = {
    TriggerLogLevel.DEBUG: logging.DEBUG,
    TriggerLogLevel.INFO: logging.INFO,
    TriggerLogLevel.WARN: logging.WARNING,
    TriggerLogLevel.ERROR: logging.ERROR,
}


class StandardLoggingTriggerLogger:
    """The default Trigger logger: writes records through the standard `logging` module.

    Without a logger it uses `logging.getLogger("dex_connectors.sdk.trigger")`. The SDK never configures
    levels or handlers, so INFO and DEBUG records stay hidden until the host sets that logger's level and
    adds a handler. Each record's message is the Trigger message followed by its attributes as compact
    sorted JSON; `extra` carries `trigger_message` and `trigger_attributes` for structured handlers.
    """

    def __init__(self, logger: logging.Logger | None = None) -> None:
        """Write to logger, or to the `dex_connectors.sdk.trigger` logger when it is None."""
        self._logger = logger if logger is not None else logging.getLogger("dex_connectors.sdk.trigger")

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        """Delegate to the standard logger's effective level."""
        return self._logger.isEnabledFor(_STANDARD_LEVELS[level])

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        """Write one record at the mapped standard level."""
        extra = {"trigger_message": message, "trigger_attributes": dict(attributes)}
        if not attributes:
            self._logger.log(_STANDARD_LEVELS[level], "%s", message, extra=extra, stacklevel=3)
            return
        rendered = json.dumps(dict(attributes), sort_keys=True, separators=(",", ":"), ensure_ascii=False)
        self._logger.log(_STANDARD_LEVELS[level], "%s %s", message, rendered, extra=extra, stacklevel=3)


class _AttributeBindingTriggerLogger:
    def __init__(self, base: TriggerLogger, attributes: Mapping[str, TriggerLogValue]) -> None:
        self._base = base
        self._attributes = dict(attributes)

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        return self._base.is_enabled_for(level)

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        self._base.log(level, message, {**self._attributes, **attributes})


_DEFAULT_TRIGGER_LOGGER: Final = StandardLoggingTriggerLogger()


def bind_trigger_log_attributes(
    logger: TriggerLogger | None, attributes: Mapping[str, TriggerLogValue]
) -> TriggerLogger:
    """Return a logger that adds attributes to every record; a record keeps its own value for a shared key.

    None binds the default StandardLoggingTriggerLogger.
    """
    return _AttributeBindingTriggerLogger(
        logger if logger is not None else _DEFAULT_TRIGGER_LOGGER, attributes
    )


class TriggerContext:
    """Per-delivery cancellation, log attributes, and skip recording, passed to every target call.

    Create the root with `TriggerContext()` and derive attempts with `start_trigger_attempt`.
    """

    def __init__(self, cancellation: TriggerCancellation | None = None) -> None:
        """Create a root context; without a cancellation it is never cancelled."""
        self._cancellation = cancellation if cancellation is not None else TriggerCancellation()
        self._log_attributes: Mapping[str, TriggerLogValue] = MappingProxyType({})
        self._on_skip: Callable[[], None] | None = None

    @classmethod
    def _derive(
        cls,
        cancellation: TriggerCancellation,
        attributes: Mapping[str, TriggerLogValue],
        on_skip: Callable[[], None],
    ) -> "TriggerContext":
        derived = cls(cancellation)
        derived._log_attributes = MappingProxyType(dict(attributes))
        derived._on_skip = on_skip
        return derived

    @property
    def cancellation(self) -> TriggerCancellation:
        """Ends delivery: sources stop, retries stop waiting, and late inbox writes are refused."""
        return self._cancellation

    @property
    def log_attributes(self) -> Mapping[str, TriggerLogValue]:
        """Event-scoped IDs, such as a channel ID, added to every delivery record; read-only."""
        return self._log_attributes

    def with_log_attributes(self, attributes: Mapping[str, TriggerLogValue]) -> "TriggerContext":
        """Return a context whose records also carry attributes; never pass message text or payloads."""
        return TriggerContext._derive(
            self._cancellation, {**self._log_attributes, **attributes}, self.record_skip
        )

    def record_skip(self) -> None:
        """Record that a component in this attempt consumed the event as undeliverable and logged it."""
        if self._on_skip is not None:
            self._on_skip()


@dataclass(slots=True)
class _SkipFlag:
    is_skipped: bool = False


@dataclass(frozen=True, slots=True)
class TriggerAttempt:
    """One delivery attempt started with `start_trigger_attempt`."""

    context: TriggerContext
    """The context to pass to `handle_trigger` for this attempt."""
    _flag: _SkipFlag = field(default_factory=_SkipFlag, repr=False, compare=False)

    def was_skipped(self) -> bool:
        """Report whether a component inside the attempt consumed the event as undeliverable."""
        return self._flag.is_skipped


def start_trigger_attempt(context: TriggerContext) -> TriggerAttempt:
    """Start one delivery attempt for a source that retries on its own schedule.

    The attempt shares the context's cancellation and attributes; its `record_skip` also reaches context.
    """
    flag = _SkipFlag()

    def record_skip() -> None:
        flag.is_skipped = True
        context.record_skip()

    attempt_context = TriggerContext._derive(context.cancellation, context.log_attributes, record_skip)
    return TriggerAttempt(attempt_context, flag)


def log_trigger_record(
    logger: TriggerLogger | None,
    context: TriggerContext,
    level: TriggerLogLevel,
    message: str,
    attributes: Mapping[str, TriggerLogValue] | None = None,
) -> None:
    """Log one record with the context's attributes; record attributes win and logger errors are ignored."""
    target = logger if logger is not None else _DEFAULT_TRIGGER_LOGGER
    try:
        if not target.is_enabled_for(level):
            return
        target.log(level, message, {**context.log_attributes, **(attributes or {})})
    except Exception:  # noqa: BLE001
        # A logger failure cannot change Trigger delivery state.
        return


def _trigger_error_type(error: BaseException) -> str:
    name = type(error).__name__
    return name if _ERROR_TYPE_NAME.fullmatch(name) else "Error"


def trigger_error_attributes(error: BaseException) -> dict[str, TriggerLogValue]:
    """Record attributes for an error: its class, its cause's class, and a gRPC or integer code.

    Messages are never included, because handlers, providers, and credential stores may put secrets or
    payload text in them.
    """
    attributes: dict[str, TriggerLogValue] = {"error_type": _trigger_error_type(error)}
    if error.__cause__ is not None:
        attributes["error_cause_type"] = _trigger_error_type(error.__cause__)
    code = getattr(error, "code", None)
    if isinstance(code, Enum) and isinstance(code.value, tuple) and isinstance(code.value[0], int):
        # A grpc.StatusCode value is (number, name).
        attributes["error_code"] = code.value[0]
    elif isinstance(code, int) and not isinstance(code, bool):
        attributes["error_code"] = code
    return attributes


def _trigger_retry_delay(attempt: int) -> timedelta:
    delay = _RETRY_INITIAL_DELAY
    completed = 1
    while completed < attempt and delay < _RETRY_MAXIMUM_DELAY:
        delay *= 2
        completed += 1
    return min(delay, _RETRY_MAXIMUM_DELAY)


@dataclass(frozen=True, slots=True)
class TriggerEvent(Generic[PayloadT_co]):
    """One provider event with a stable provider identity."""

    id: str
    """Stable provider identifier; also the Flow-start request ID."""
    occurred_at: datetime
    """When the provider event occurred, timezone-aware."""
    payload: PayloadT_co
    """Connector-decoded provider event body."""


class TriggerTarget(Protocol[PayloadT_contra]):
    """Handles one event after the provider transport acknowledges receipt.

    `handle_trigger` returns when it consumes the event, raises UndeliverableTriggerError when no retry
    can deliver it, or raises another exception to keep the event pending for a retry.
    """

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[PayloadT_contra]) -> None:
        """Consume, retry, or mark one provider event undeliverable."""
        ...


@runtime_checkable
class TriggerDeliveryPreparer(Protocol[PayloadT_contra]):
    """A target that persists an event before the provider acknowledges it."""

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[PayloadT_contra]) -> None:
        """Persist an event before provider acknowledgement."""
        ...


@runtime_checkable
class TriggerDeliveryReplayer(Protocol):
    """A target that redelivers events persisted before a previous process stopped."""

    async def replay_trigger_deliveries(self, context: TriggerContext) -> None:
        """Deliver every pending event in order, returning once all are consumed."""
        ...


async def prepare_trigger_delivery(
    context: TriggerContext, target: TriggerTarget[PayloadT], event: TriggerEvent[PayloadT]
) -> None:
    """Ask a durable target to persist an event before acknowledgement; other targets need nothing."""
    if isinstance(target, TriggerDeliveryPreparer):
        await target.prepare_trigger(context, event)


def has_durable_trigger_inbox(target: object) -> bool:
    """Report whether a target both records and replays events, as `new_durable_trigger_target` builds."""
    return isinstance(target, TriggerDeliveryPreparer) and isinstance(target, TriggerDeliveryReplayer)


async def _sleep(delay: timedelta) -> None:
    await asyncio.sleep(delay.total_seconds())


async def _wait_for_trigger_delay(cancellation: TriggerCancellation, delay: timedelta) -> None:
    cancellation.raise_if_cancelled()
    sleeping = asyncio.ensure_future(_sleep(delay))
    cancelled = asyncio.ensure_future(cancellation.wait())
    try:
        await asyncio.wait({sleeping, cancelled}, return_when=asyncio.FIRST_COMPLETED)
    finally:
        for task in (sleeping, cancelled):
            if not task.done():
                task.cancel()
    cancellation.raise_if_cancelled()


async def deliver_trigger(
    context: TriggerContext,
    target: TriggerTarget[PayloadT],
    event: TriggerEvent[PayloadT],
    *,
    logger: TriggerLogger | None = None,
) -> None:
    """Hand one acknowledged event to target until target consumes it.

    A returned attempt or an UndeliverableTriggerError consumes the event. Every other `Exception` is
    retried after a delay that starts at 250 milliseconds and doubles up to 30 seconds. It raises
    TriggerDeliveryAbortedError as soon as the context is cancelled, including during a wait;
    `asyncio.CancelledError` propagates unchanged.

    It logs WARN "trigger delivery failed; retrying" before every wait, INFO "trigger delivered after
    retry" when a later attempt succeeds, and WARN "trigger event skipped: undeliverable" when the target
    raises the marker. A target that consumes an undeliverable event itself logs that skip instead.
    """
    attempt_number = 1
    while True:
        context.cancellation.raise_if_cancelled()
        attempt = start_trigger_attempt(context)
        try:
            await target.handle_trigger(attempt.context, event)
        except Exception as error:
            if is_trigger_undeliverable(error):
                log_trigger_record(
                    logger,
                    context,
                    TriggerLogLevel.WARN,
                    "trigger event skipped: undeliverable",
                    {"event_id": event.id, **trigger_error_attributes(error)},
                )
                return
            # The attempt failed because delivery ended, so there is no retry to report.
            context.cancellation.raise_if_cancelled()
            delay = _trigger_retry_delay(attempt_number)
            log_trigger_record(
                logger,
                context,
                TriggerLogLevel.WARN,
                "trigger delivery failed; retrying",
                {
                    "event_id": event.id,
                    "attempt": attempt_number,
                    "delay_ms": delay // timedelta(milliseconds=1),
                    **trigger_error_attributes(error),
                },
            )
            await _wait_for_trigger_delay(context.cancellation, delay)
            attempt_number += 1
            continue
        if attempt_number > 1 and not attempt.was_skipped():
            log_trigger_record(
                logger,
                context,
                TriggerLogLevel.INFO,
                "trigger delivered after retry",
                {"event_id": event.id, "attempts": attempt_number},
            )
        return
