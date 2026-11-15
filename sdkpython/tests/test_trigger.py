# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Trigger delivery, runner, and Dex target behavior ported from the TypeScript and Go SDKs."""

import asyncio
import json
import logging
import pickle
from collections.abc import Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any, cast

import dex
import grpc  # type: ignore[import-untyped]
import pytest
from dex.runtime_errors import ErrorSubStatus

from dex_connectors.sdk import (
    UNDELIVERABLE_TRIGGER_ERROR_TYPE,
    ConnectionRef,
    StandardLoggingTriggerLogger,
    TriggerBindingDefinition,
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
    UndeliverableTriggerError,
    classify_dex_trigger_error,
    deliver_trigger,
    is_trigger_undeliverable,
    mark_trigger_undeliverable,
    must_trigger_binding_definition,
    new_dex_flow_trigger_target,
    new_dex_rpc_trigger_target,
    new_trigger,
    start_trigger_attempt,
    trigger_error_attributes,
)
from dex_connectors.sdk import trigger_delivery as delivery_module
from dex_connectors.sdk.trigger import _is_trigger_delivery_aborted


@dataclass(frozen=True, slots=True)
class RecordedLog:
    """One captured Trigger record."""

    level: TriggerLogLevel
    message: str
    attributes: dict[str, TriggerLogValue]


@dataclass(slots=True)
class RecordingTriggerLogger:
    """A logger that keeps every record for assertions."""

    records: list[RecordedLog] = field(default_factory=list)

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        """Enable every level."""
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        """Keep the record."""
        self.records.append(RecordedLog(level, message, dict(attributes)))

    def messages(self) -> list[str]:
        """Return the record messages in order."""
        return [record.message for record in self.records]


EVENT: TriggerEvent[dict[str, str]] = TriggerEvent(
    "Ev1", datetime(2025, 10, 12, tzinfo=UTC), {"text": "SENTINEL payload"}
)
DEFINITION = TriggerDefinition(TriggerRef("sample", "itemCreated"), "Receive a new item.")
BINDING = TriggerBindingRef(ConnectionRef("sample", "workspace"), DEFINITION.trigger, "start")


@pytest.fixture(autouse=True)
def fast_retry_waits(monkeypatch: pytest.MonkeyPatch) -> list[timedelta]:
    """Record retry waits instead of sleeping, so tests never wait on the wall clock."""
    waits: list[timedelta] = []

    async def record_wait(delay: timedelta) -> None:
        waits.append(delay)
        await asyncio.sleep(0)

    monkeypatch.setattr(delivery_module, "_sleep", record_wait)
    return waits


class ProviderTimeoutError(Exception):
    """A retryable provider failure for tests."""


class ScriptedTarget:
    """A target whose attempts follow a script of exceptions."""

    def __init__(self, failures: list[BaseException | None], *, skip_on_success: bool = False) -> None:
        self.failures = failures
        self.attempts = 0
        self.skip_on_success = skip_on_success

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        self.attempts += 1
        failure = self.failures.pop(0) if self.failures else None
        if failure is not None:
            raise failure
        if self.skip_on_success:
            context.record_skip()


def test_retry_delay_schedule_doubles_to_the_cap() -> None:
    delays = [delivery_module._trigger_retry_delay(attempt) for attempt in (1, 2, 3, 8, 9)]
    assert [delay.total_seconds() for delay in delays] == [0.25, 0.5, 1, 30, 30]


async def test_retries_and_logs_recovery_without_messages(fast_retry_waits: list[timedelta]) -> None:
    logger = RecordingTriggerLogger()
    cause = TypeError("SENTINEL cause")
    failure = ProviderTimeoutError("token=SENTINEL-SECRET payload=SENTINEL payload")
    failure.__cause__ = cause
    target = ScriptedTarget([failure])
    context = TriggerContext().with_log_attributes({"channel": "C1"})
    await deliver_trigger(context, target, EVENT, logger=logger)
    assert target.attempts == 2
    assert logger.messages() == ["trigger delivery failed; retrying", "trigger delivered after retry"]
    assert logger.records[0].attributes == {
        "event_id": "Ev1",
        "attempt": 1,
        "delay_ms": 250,
        "channel": "C1",
        "error_type": "ProviderTimeoutError",
        "error_cause_type": "TypeError",
    }
    assert logger.records[1].attributes == {"event_id": "Ev1", "attempts": 2, "channel": "C1"}
    assert fast_retry_waits == [timedelta(milliseconds=250)]
    assert "SENTINEL" not in json.dumps([record.attributes for record in logger.records])


async def test_consumes_undeliverable_and_aborts_waits() -> None:
    logger = RecordingTriggerLogger()
    marked = mark_trigger_undeliverable(ValueError("SENTINEL secret reason"))
    await deliver_trigger(TriggerContext(), ScriptedTarget([marked]), EVENT, logger=logger)
    assert [(record.level, record.message, record.attributes) for record in logger.records] == [
        (
            TriggerLogLevel.WARN,
            "trigger event skipped: undeliverable",
            {"event_id": "Ev1", "error_type": "UndeliverableTriggerError", "error_cause_type": "ValueError"},
        )
    ]
    assert "SENTINEL" not in json.dumps([record.attributes for record in logger.records])


async def test_cancellation_during_a_real_wait_aborts(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(delivery_module, "_sleep", lambda delay: asyncio.sleep(3600))
    cancellation = TriggerCancellation()
    target = ScriptedTarget([RuntimeError("retry")] * 5)
    pending = asyncio.ensure_future(deliver_trigger(TriggerContext(cancellation), target, EVENT))
    while target.attempts == 0:
        await asyncio.sleep(0)
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError, match="Trigger delivery was aborted"):
        await pending
    assert target.attempts == 1


async def test_task_cancellation_propagates_unchanged(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(delivery_module, "_sleep", lambda delay: asyncio.sleep(3600))
    target = ScriptedTarget([RuntimeError("retry")] * 5)
    pending = asyncio.ensure_future(deliver_trigger(TriggerContext(), target, EVENT))
    while target.attempts == 0:
        await asyncio.sleep(0)
    pending.cancel()
    with pytest.raises(asyncio.CancelledError):
        await pending


async def test_no_retry_record_after_cancellation() -> None:
    logger = RecordingTriggerLogger()
    cancellation = TriggerCancellation()

    class CancellingTarget:
        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
            cancellation.cancel()
            raise RuntimeError("ended")

    with pytest.raises(TriggerDeliveryAbortedError):
        await deliver_trigger(TriggerContext(cancellation), CancellingTarget(), EVENT, logger=logger)
    assert logger.records == []


def test_marks_once_and_finds_wrapped_markers() -> None:
    marked = mark_trigger_undeliverable(ValueError("reason"))
    assert str(marked) == "Trigger event is undeliverable: reason"
    assert mark_trigger_undeliverable(marked) is marked
    outer = RuntimeError("outer")
    outer.__cause__ = marked
    assert is_trigger_undeliverable(outer)
    assert not is_trigger_undeliverable(RuntimeError("plain"))
    assert str(UndeliverableTriggerError()) == "Trigger event is undeliverable"
    assert str(UndeliverableTriggerError(ValueError(""))) == "Trigger event is undeliverable"
    assert str(UndeliverableTriggerError("closed")) == "Trigger event is undeliverable: closed"


def _cause_chain(depth: int, innermost: BaseException) -> BaseException:
    current = innermost
    for _ in range(depth - 1):
        wrapper = RuntimeError("wrapper")
        wrapper.__cause__ = current
        current = wrapper
    return current


def test_cause_search_stops_after_32_links() -> None:
    assert is_trigger_undeliverable(_cause_chain(32, UndeliverableTriggerError()))
    assert not is_trigger_undeliverable(_cause_chain(33, UndeliverableTriggerError()))
    context_only = RuntimeError("outer")
    context_only.__context__ = UndeliverableTriggerError()
    assert not is_trigger_undeliverable(context_only)


def test_marker_reports_the_shared_worker_type_and_pickles_instances() -> None:
    marker = UndeliverableTriggerError(ValueError("closed"))
    assert f"{type(marker).__module__}.{type(marker).__qualname__}" == UNDELIVERABLE_TRIGGER_ERROR_TYPE
    restored = pickle.loads(pickle.dumps(marker))
    assert type(restored) is UndeliverableTriggerError
    assert str(restored) == str(marker)
    assert restored.cause == "closed"
    with pytest.raises(pickle.PicklingError):
        pickle.dumps(UndeliverableTriggerError)


def test_error_attributes_include_grpc_and_integer_codes() -> None:
    service = dex.DexServiceError(
        grpc.StatusCode.INVALID_ARGUMENT, ErrorSubStatus.UNCATEGORIZED, "x", "op", None
    )
    assert trigger_error_attributes(service) == {"error_type": "DexServiceError", "error_code": 3}
    coded = OSError("x")
    coded.code = 7  # type: ignore[attr-defined]
    assert trigger_error_attributes(coded)["error_code"] == 7
    flagged = OSError("x")
    flagged.code = True  # type: ignore[attr-defined]
    assert "error_code" not in trigger_error_attributes(flagged)


async def test_skip_inside_an_attempt_suppresses_recovery_record() -> None:
    logger = RecordingTriggerLogger()
    target = ScriptedTarget([RuntimeError("retry")], skip_on_success=True)
    await deliver_trigger(TriggerContext(), target, EVENT, logger=logger)
    assert logger.messages() == ["trigger delivery failed; retrying"]
    attempt = start_trigger_attempt(TriggerContext())
    attempt.context.record_skip()
    assert attempt.was_skipped()


def test_standard_logging_adapter_writes_sorted_attributes(caplog: pytest.LogCaptureFixture) -> None:
    adapter = StandardLoggingTriggerLogger()
    with caplog.at_level(logging.INFO, logger="dex_connectors.sdk.trigger"):
        assert not adapter.is_enabled_for(TriggerLogLevel.DEBUG)
        adapter.log(TriggerLogLevel.WARN, "trigger delivery failed; retrying", {"z": 1, "a": "x"})
        adapter.log(TriggerLogLevel.INFO, "plain", {})
    assert [record.getMessage() for record in caplog.records] == [
        'trigger delivery failed; retrying {"a":"x","z":1}',
        "plain",
    ]
    assert caplog.records[0].levelno == logging.WARNING
    assert caplog.records[0].__dict__["trigger_attributes"] == {"z": 1, "a": "x"}


class OrderRecordingTarget:
    """A durable-looking target that records call order and rejects every event."""

    def __init__(self, calls: list[str], replay_failure: BaseException | None = None) -> None:
        self.calls = calls
        self.replay_failure = replay_failure

    async def replay_trigger_deliveries(self, context: TriggerContext) -> None:
        self.calls.append("replay")
        if self.replay_failure is not None:
            raise self.replay_failure

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        self.calls.append(f"prepare:{event.id}")

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        self.calls.append("handle")
        raise mark_trigger_undeliverable(LookupError("no Flow"))


class OneEventSource:
    """A source that prepares and delivers one event in one attempt."""

    def __init__(self, calls: list[str]) -> None:
        self.calls = calls

    async def run(self, context: TriggerContext, target: TriggerTarget[Any]) -> None:
        from dex_connectors.sdk import prepare_trigger_delivery

        await prepare_trigger_delivery(context, target, EVENT)
        attempt = start_trigger_attempt(context)
        await target.handle_trigger(attempt.context, EVENT)
        self.calls.append(f"skipped:{attempt.was_skipped()}")


async def test_runner_replays_prepares_and_consumes_with_binding_identity() -> None:
    calls: list[str] = []
    logger = RecordingTriggerLogger()
    runner = new_trigger(
        TriggerConfig(DEFINITION, BINDING, OneEventSource(calls), OrderRecordingTarget(calls), logger)
    )
    await runner.run()
    assert calls == ["replay", "prepare:Ev1", "handle", "skipped:True"]
    assert logger.records[0].message == "trigger event skipped: undeliverable"
    assert logger.records[0].attributes == {
        "connector": "sample",
        "connection": "workspace",
        "trigger": "itemCreated",
        "binding": "start",
        "event_id": "Ev1",
        "error_type": "UndeliverableTriggerError",
        "error_cause_type": "LookupError",
    }
    assert runner.binding == BINDING
    assert runner.definition == DEFINITION


async def test_runner_wraps_replay_failures_including_aborts() -> None:
    calls: list[str] = []
    failing = new_trigger(
        TriggerConfig(
            DEFINITION, BINDING, OneEventSource(calls), OrderRecordingTarget(calls, OSError("storage"))
        )
    )
    with pytest.raises(RuntimeError, match="^replay Trigger deliveries: storage$") as raised:
        await failing.run()
    assert isinstance(raised.value.__cause__, OSError)
    assert not _is_trigger_delivery_aborted(raised.value)
    aborted = new_trigger(
        TriggerConfig(
            DEFINITION,
            BINDING,
            OneEventSource(calls),
            OrderRecordingTarget(calls, TriggerDeliveryAbortedError()),
        )
    )
    with pytest.raises(RuntimeError, match="replay Trigger deliveries") as wrapped:
        await aborted.run()
    assert _is_trigger_delivery_aborted(wrapped.value)
    cancelled = new_trigger(
        TriggerConfig(
            DEFINITION, BINDING, OneEventSource(calls), OrderRecordingTarget(calls, asyncio.CancelledError())
        )
    )
    with pytest.raises(asyncio.CancelledError):
        await cancelled.run()


def test_rejects_mismatched_bindings_and_invalid_metadata() -> None:
    other = TriggerBindingRef(BINDING.connection, TriggerRef("sample", "other"), "start")
    calls: list[str] = []
    with pytest.raises(ValueError, match="does not match"):
        new_trigger(TriggerConfig(DEFINITION, other, OneEventSource(calls), OrderRecordingTarget(calls)))
    with pytest.raises(ValueError, match="source and target are required"):
        new_trigger(TriggerConfig(DEFINITION, BINDING, cast(Any, None), OrderRecordingTarget(calls)))
    with pytest.raises(ValueError):
        must_trigger_binding_definition(TriggerBindingDefinition(DEFINITION, " ", "start"))
    assert (
        must_trigger_binding_definition(
            TriggerBindingDefinition(DEFINITION, "workspace", "start")
        ).binding_name
        == "start"
    )
    for invalid in (
        TriggerRef("slack\n", "itemCreated"),
        TriggerRef("sample", "itemCreated\n"),
        TriggerRef("S", "x"),
    ):
        with pytest.raises(ValueError):
            invalid.validate()
    with pytest.raises(ValueError):
        TriggerDefinition(DEFINITION.trigger, "  ").validate()


class FakeFlowClient:
    """Records start_flow calls; later starts report an already started Flow."""

    def __init__(self, failure: BaseException | None = None) -> None:
        self.starts: list[tuple[Any, ...]] = []
        self.is_started = False
        self.failure = failure

    async def start_flow(
        self, flow: Any, flow_id: str, flow_input: Any, options: dex.StartFlowOptions
    ) -> str:
        self.starts.append((flow, flow_id, flow_input, options))
        if self.failure is not None:
            raise self.failure
        if self.is_started:
            raise dex.FlowAlreadyStartedError(
                grpc.StatusCode.ALREADY_EXISTS,
                ErrorSubStatus.FLOW_ALREADY_STARTED,
                "exists",
                "startFlow",
                flow_id,
            )
        self.is_started = True
        return "run"


FLOW = cast(dex.Flow[dict[str, str]], object())


def _flow_target(
    client: Any, logger: RecordingTriggerLogger, *, accepts: bool = True, flow_id: str | None = None
) -> TriggerTarget[dict[str, str]]:
    return new_dex_flow_trigger_target(
        cast(dex.AsyncClient, client),
        FLOW,
        accepts_event=lambda event: accepts,
        resolve_flow_id=lambda event: flow_id if flow_id is not None else f"flow-{event.id}",
        map_to_flow_input=lambda event: event.payload,
        logger=logger,
    )


async def test_flow_target_starts_once_and_treats_duplicates_as_delivered() -> None:
    client = FakeFlowClient()
    logger = RecordingTriggerLogger()
    target = _flow_target(client, logger)
    await target.handle_trigger(TriggerContext(), EVENT)
    await target.handle_trigger(TriggerContext(), EVENT)
    flow, flow_id, flow_input, options = client.starts[0]
    assert (flow, flow_id, flow_input) == (FLOW, "flow-Ev1", EVENT.payload)
    assert options == dex.StartFlowOptions(id_reuse_policy=dex.IdReusePolicy.DISALLOW, request_id="Ev1")
    assert [record.attributes["duplicate"] for record in logger.records] == [False, True]
    assert logger.records[0].attributes == {
        "target": "flow_start",
        "event_id": "Ev1",
        "flow_id": "flow-Ev1",
        "duplicate": False,
    }
    with pytest.raises(UndeliverableTriggerError, match="Flow trigger requires Flow ID and event ID"):
        await _flow_target(client, logger, flow_id=" ").handle_trigger(TriggerContext(), EVENT)
    filtered = RecordingTriggerLogger()
    await _flow_target(client, filtered, accepts=False).handle_trigger(TriggerContext(), EVENT)
    assert filtered.messages() == ["trigger event skipped: filtered"]
    assert len(client.starts) == 2


async def test_flow_target_classifies_start_failures() -> None:
    logger = RecordingTriggerLogger()
    encode_failure = _flow_target(FakeFlowClient(dex.ValueMappingError("encode", "bad")), logger)
    with pytest.raises(UndeliverableTriggerError):
        await encode_failure.handle_trigger(TriggerContext(), EVENT)
    unavailable = _flow_target(FakeFlowClient(OSError("unavailable")), logger)
    with pytest.raises(OSError, match="unavailable"):
        await unavailable.handle_trigger(TriggerContext(), EVENT)


def _worker_failure(code: grpc.StatusCode, error_type: str, detail: str) -> dex.WorkerInvocationError:
    return dex.WorkerInvocationError(
        grpc.StatusCode.UNKNOWN,
        ErrorSubStatus.WORKER_API_ERROR,
        "x",
        "invokeRPC",
        "f",
        code,
        error_type,
        detail,
    )


def test_classifier_accepts_only_exact_handler_markers() -> None:
    thrown = UndeliverableTriggerError(ValueError("closed"))
    handler_marker = _worker_failure(grpc.StatusCode.UNKNOWN, UNDELIVERABLE_TRIGGER_ERROR_TYPE, str(thrown))
    bare_marker = _worker_failure(
        grpc.StatusCode.UNKNOWN, UNDELIVERABLE_TRIGGER_ERROR_TYPE, "Trigger event is undeliverable"
    )
    other = _worker_failure(grpc.StatusCode.UNKNOWN, "builtins.RuntimeError", "thread is still loading")
    assert is_trigger_undeliverable(classify_dex_trigger_error(handler_marker))
    assert is_trigger_undeliverable(classify_dex_trigger_error(bare_marker))
    assert classify_dex_trigger_error(other) is other
    for lookalike in (
        _worker_failure(
            grpc.StatusCode.FAILED_PRECONDITION, "error", "Trigger event is undeliverable: closed"
        ),
        _worker_failure(
            grpc.StatusCode.UNKNOWN, "UndeliverableTriggerError", "Trigger event is undeliverable: closed"
        ),
        _worker_failure(
            grpc.StatusCode.UNKNOWN, "FlowNotActiveOrNotFoundError", "Trigger event is undeliverable: closed"
        ),
        _worker_failure(
            grpc.StatusCode.UNKNOWN,
            UNDELIVERABLE_TRIGGER_ERROR_TYPE,
            "Trigger event is undeliverable because",
        ),
        _worker_failure(
            grpc.StatusCode.UNKNOWN, UNDELIVERABLE_TRIGGER_ERROR_TYPE, "error: Trigger event is undeliverable"
        ),
    ):
        assert not is_trigger_undeliverable(classify_dex_trigger_error(lookalike)), (
            lookalike.worker_error_detail
        )
    missing = dex.FlowNotActiveOrNotFoundError(
        grpc.StatusCode.NOT_FOUND, ErrorSubStatus.FLOW_NOT_EXISTS, "gone", "invokeRPC", "f"
    )
    assert is_trigger_undeliverable(classify_dex_trigger_error(missing))
    assert is_trigger_undeliverable(classify_dex_trigger_error(dex.ValueMappingError("encode", "bad")))
    generic = RuntimeError("unavailable")
    assert classify_dex_trigger_error(generic) is generic
    assert classify_dex_trigger_error(thrown) is thrown


class FakeRPCClient:
    """Records invoke_rpc calls and raises scripted failures."""

    def __init__(self, failures: list[BaseException]) -> None:
        self.failures = failures
        self.calls: list[tuple[tuple[Any, ...], dict[str, Any]]] = []

    async def invoke_rpc(self, *arguments: Any, **keywords: Any) -> None:
        self.calls.append((arguments, keywords))
        if self.failures:
            raise self.failures.pop(0)


class RPCFlow:
    """Stand-in Flow with RPC handlers of both shapes."""

    async def send_reply(self, ctx: Any, reply: str) -> None:
        """Take an input."""

    async def describe_status(self, ctx: Any) -> None:
        """Take no input."""


async def test_rpc_target_delivers_undecodable_responses_and_never_sends_request_ids() -> None:
    other = _worker_failure(grpc.StatusCode.UNKNOWN, "builtins.RuntimeError", "loading")
    client = FakeRPCClient([dex.ValueMappingError("decode", "bad output"), other])
    logger = RecordingTriggerLogger()
    flow = RPCFlow()
    target: TriggerTarget[dict[str, str]] = new_dex_rpc_trigger_target(
        cast(dex.AsyncClient, client),
        flow.send_reply,
        accepts_event=lambda event: True,
        resolve_flow_id=lambda event: "flow-1",
        map_to_rpc_input=lambda event: event.payload["text"],
        logger=logger,
    )
    await target.handle_trigger(TriggerContext(), EVENT)
    assert logger.records[0].message == "trigger event delivered; rpc response undecodable"
    assert logger.records[0].attributes == {
        "target": "rpc",
        "event_id": "Ev1",
        "flow_id": "flow-1",
        "error_type": "ValueMappingError",
    }
    with pytest.raises(dex.WorkerInvocationError):
        await target.handle_trigger(TriggerContext(), EVENT)
    await target.handle_trigger(TriggerContext(), EVENT)
    assert logger.records[-1].attributes == {"target": "rpc", "event_id": "Ev1", "flow_id": "flow-1"}
    assert all(call == ((flow.send_reply, "flow-1", "SENTINEL payload"), {}) for call in client.calls)
    no_input = FakeRPCClient([])
    input_free: TriggerTarget[dict[str, str]] = new_dex_rpc_trigger_target(
        cast(dex.AsyncClient, no_input),
        flow.describe_status,
        accepts_event=lambda event: True,
        resolve_flow_id=lambda event: "flow-1",
    )
    await input_free.handle_trigger(TriggerContext(), EVENT)
    assert no_input.calls == [((flow.describe_status, "flow-1"), {})]
    with pytest.raises(UndeliverableTriggerError, match="RPC trigger requires Flow ID and event ID"):
        blank: TriggerTarget[dict[str, str]] = new_dex_rpc_trigger_target(
            cast(dex.AsyncClient, no_input),
            flow.describe_status,
            accepts_event=lambda event: True,
            resolve_flow_id=lambda event: "",
        )
        await blank.handle_trigger(TriggerContext(), EVENT)


def test_target_constructors_reject_missing_arguments_and_missing_mappers() -> None:
    client = cast(dex.AsyncClient, FakeRPCClient([]))
    flow = RPCFlow()
    with pytest.raises(ValueError, match="requires an RPC input mapper"):
        new_dex_rpc_trigger_target(
            client,
            cast(Any, flow.send_reply),
            accepts_event=lambda event: True,
            resolve_flow_id=lambda event: "f",
        )
    with pytest.raises(
        ValueError, match="Dex client, RPC, Trigger filter, and Flow ID resolver are required"
    ):
        new_dex_rpc_trigger_target(
            cast(Any, None),
            flow.describe_status,
            accepts_event=lambda event: True,
            resolve_flow_id=lambda event: "f",
        )
    message = "Dex client, Flow, Trigger filter, Flow ID resolver, and Flow input mapper are required"
    with pytest.raises(ValueError, match=message):
        new_dex_flow_trigger_target(
            client,
            FLOW,
            accepts_event=lambda event: True,
            resolve_flow_id=lambda event: "f",
            map_to_flow_input=cast(Any, None),
        )
