# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Webhook endpoint pipeline, arrival order, ASGI serving, and the endpoint runner."""

import asyncio
import json
from collections.abc import Callable, Mapping, MutableMapping
from dataclasses import dataclass, field
from datetime import timedelta
from typing import Any

import pytest

from dex_connectors.sdk import (
    Call,
    ConnectionRef,
    StaticCredentialProvider,
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
from dex_connectors.sdk.projectconfig import (
    CredentialBrokerUnavailableError,
    InMemoryObjectStore,
    RelaySignatureInvalidError,
    RelayTokenRenewalDeferredError,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    TriggerJSONPayload,
    new_durable_trigger_target,
)
from dex_connectors.sdk.relaysignature import RelayHeaders
from dex_connectors.sdk.webhooktrigger import (
    WebhookEndpoint,
    WebhookEndpointConfig,
    WebhookEndpointRunner,
    WebhookRequest,
    WebhookResponse,
    WebhookVerificationUnavailableError,
    new_relayed_request_verification,
)


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


CONNECTION = ConnectionRef("sample", "workspace")
Payload = dict[str, str]


class ManualTimers:
    """A timer seam that fires slot expiries only when a test asks."""

    def __init__(self) -> None:
        self.pending: list[tuple[float, Callable[[], None], ManualTimers.Handle]] = []

    class Handle:
        def __init__(self) -> None:
            self.is_cancelled = False

        def cancel(self) -> None:
            self.is_cancelled = True

    def schedule(self, delay: float, callback: Callable[[], None]) -> "ManualTimers.Handle":
        handle = ManualTimers.Handle()
        self.pending.append((delay, callback, handle))
        return handle

    def fire(self, index: int) -> None:
        _, callback, handle = self.pending[index]
        if not handle.is_cancelled:
            callback()


async def _allow(
    request: WebhookRequest, credentials: dict[str, str], cancellation: TriggerCancellation
) -> None:
    return None


def _decode(request: WebhookRequest) -> TriggerEvent[Payload] | None:
    body = json.loads(request.body)
    if body.get("text") == "ignored":
        return None
    if not body.get("id"):
        raise ValueError("missing ID")
    return TriggerEvent(body["id"], request.received_at, {"text": body["text"]})


def _endpoint(
    verify: Callable[[WebhookRequest, dict[str, str], TriggerCancellation], Any] = _allow,
    timers: ManualTimers | None = None,
    **overrides: Any,
) -> WebhookEndpoint[dict[str, str], Payload]:
    arguments: dict[str, Any] = {
        "connector_id": "sample",
        "trigger_name": "itemCreated",
        "connection": CONNECTION,
        "credentials": StaticCredentialProvider({CONNECTION: {"secret": "s"}}),
        "verify_request": verify,
        "decode_event": _decode,
        "maximum_body_bytes": 64,
        "logger": RecordingTriggerLogger(),
    }
    config: WebhookEndpointConfig[dict[str, str], Payload] = WebhookEndpointConfig(
        **{**arguments, **overrides}
    )
    return WebhookEndpoint(config, _schedule=timers.schedule if timers else None)


async def _post(endpoint: Any, body: str, method: str = "POST") -> int:
    response: WebhookResponse = await endpoint.handle_request(
        method, "/hooks", [("Content-Type", "x")], body.encode()
    )
    return response.status


class GatedTarget:
    """Records preparation immediately and delivery after its gate opens."""

    def __init__(self) -> None:
        self.prepared: list[str] = []
        self.delivered: list[str] = []
        self.gate = asyncio.Event()

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
        self.prepared.append(event.id)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
        await self.gate.wait()
        self.delivered.append(event.id)


async def _until(predicate: Callable[[], bool]) -> None:
    async with asyncio.timeout(5):
        while not predicate():
            await asyncio.sleep(0.001)


async def test_answers_503_without_a_source_then_records_before_200_in_order() -> None:
    endpoint = _endpoint()
    assert await _post(endpoint, '{"id":"Ev0","text":"early"}') == 503
    target = GatedTarget()
    cancellation = TriggerCancellation()
    source = endpoint.new_source(lambda event: event.payload["text"] != "other")
    running = asyncio.ensure_future(source.run(TriggerContext(cancellation), target))
    await _until(lambda: endpoint.running_source_count() == 1)
    for body in (
        '{"id":"Ev1","text":"first"}',
        '{"id":"Ev2","text":"second"}',
        '{"id":"Ev3","text":"other"}',
        '{"text":"ignored"}',
    ):
        assert await _post(endpoint, body) == 200
    assert target.prepared == ["Ev1", "Ev2"]
    target.gate.set()
    await _until(lambda: target.delivered == ["Ev1", "Ev2"])
    get = await endpoint.handle_request("GET", "/hooks", [], b"")
    assert (get.status, dict(get.headers)["allow"], get.body) == (405, "POST", b"method not allowed\n")
    too_large = await endpoint.handle_request("POST", "/hooks", [], b"x" * 65)
    assert (too_large.status, dict(too_large.headers)["connection"]) == (413, "close")
    assert await _post(endpoint, '{"text":"no id"}') == 400
    with pytest.raises(RuntimeError, match="already running"):
        await source.run(TriggerContext(), target)
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running
    assert endpoint.running_source_count() == 0


async def test_verification_failures_map_to_400_or_503_and_credentials_use_a_stable_call() -> None:
    calls: list[Call] = []

    class RecordingProvider:
        async def resolve(self, call: Call) -> dict[str, str]:
            calls.append(call)
            return {"secret": "s"}

    async def refuse(
        request: WebhookRequest, credentials: dict[str, str], cancellation: TriggerCancellation
    ) -> None:
        raise ValueError("bad signature")

    async def unavailable(
        request: WebhookRequest, credentials: dict[str, str], cancellation: TriggerCancellation
    ) -> None:
        raise WebhookVerificationUnavailableError()

    refused = await _endpoint(refuse).handle_request("POST", "/h", [], b"{}")
    assert (refused.status, refused.body) == (400, b"invalid webhook signature\n")
    assert dict(refused.headers)["content-type"] == "text/plain; charset=utf-8"
    deferred = await _endpoint(unavailable).handle_request("POST", "/h", [], b"{}")
    assert (deferred.status, deferred.body) == (503, b"webhook temporarily unavailable\n")
    recording = _endpoint(credentials=RecordingProvider())
    await recording.handle_request("POST", "/h", [], b'{"id":"Ev1"}')
    await recording.handle_request("POST", "/h", [], b'{"id":"Ev1"}')
    assert str(calls[0].id) == "52f555ed-6fea-5c16-a247-f8edfed47dd7"
    assert calls[0] == calls[1]
    assert (calls[0].operation.connector_id, calls[0].operation.operation_id) == ("sample", "itemCreated")
    missing = await _endpoint(credentials=StaticCredentialProvider({})).handle_request(
        "POST", "/h", [], b"{}"
    )
    assert missing.status == 503
    undecodable = await _endpoint().handle_request("POST", "/h", [], b"not json")
    assert (undecodable.status, undecodable.body) == (400, b"invalid webhook event\n")


def test_rejects_invalid_endpoint_configuration() -> None:
    for overrides in (
        {"maximum_body_bytes": 0},
        {"source_queue_capacity": 0},
        {"arrival_timeout": timedelta(0)},
        {"connector_id": " "},
        {"connection": ConnectionRef("sample", " ")},
    ):
        with pytest.raises(ValueError):
            base: dict[str, Any] = {
                "connector_id": "sample",
                "trigger_name": "itemCreated",
                "connection": CONNECTION,
                "credentials": StaticCredentialProvider({}),
                "verify_request": _allow,
                "decode_event": _decode,
                "maximum_body_bytes": 64,
            }
            WebhookEndpoint(WebhookEndpointConfig(**{**base, **overrides}))


class SlowFirstRecordTarget:
    """The first prepare waits for a release; deliveries are recorded."""

    def __init__(self) -> None:
        self.release = asyncio.Event()
        self.is_first_started = asyncio.Event()
        self.prepared: list[str] = []
        self.delivered: list[str] = []

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
        if not self.prepared:
            self.prepared.append(event.id)
            self.is_first_started.set()
            await self.release.wait()
            return
        self.prepared.append(event.id)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
        self.delivered.append(event.id)


async def test_keeps_arrival_order_when_an_earlier_record_is_slower() -> None:
    endpoint = _endpoint()
    target = SlowFirstRecordTarget()
    cancellation = TriggerCancellation()
    running = asyncio.ensure_future(endpoint.new_source().run(TriggerContext(cancellation), target))
    await _until(lambda: endpoint.running_source_count() == 1)
    first = asyncio.ensure_future(_post(endpoint, '{"id":"EvFirst","text":"a"}'))
    await target.is_first_started.wait()
    second = asyncio.ensure_future(_post(endpoint, '{"id":"EvSecond","text":"b"}'))
    unverified = await _endpoint(lambda *arguments: _raise(ValueError("x"))).handle_request(
        "POST", "/h", [], b"{}"
    )
    assert unverified.status == 400
    for _ in range(50):
        await asyncio.sleep(0)
    assert target.prepared == ["EvFirst"]
    target.release.set()
    assert await first == 200 and await second == 200
    await _until(lambda: target.delivered == ["EvFirst", "EvSecond"])
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running


async def _raise(error: BaseException) -> None:
    raise error


class LateWriteTarget:
    """Writes for IDs starting with EvSlow wait for a release and ignore slot expiry."""

    def __init__(self) -> None:
        self.release = asyncio.Event()
        self.started: list[str] = []
        self.recorded: list[str] = []
        self.delivered: list[str] = []

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
        self.started.append(event.id)
        if event.id.startswith("EvSlow"):
            await self.release.wait()
        self.recorded.append(event.id)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
        self.delivered.append(event.id)


async def test_slots_expire_from_reservation_and_late_writes_still_queue() -> None:
    timers = ManualTimers()
    endpoint = _endpoint(timers=timers)
    target = LateWriteTarget()
    cancellation = TriggerCancellation()
    running = asyncio.ensure_future(endpoint.new_source().run(TriggerContext(cancellation), target))
    await _until(lambda: endpoint.running_source_count() == 1)
    first = asyncio.ensure_future(_post(endpoint, '{"id":"EvSlowFirst","text":"x"}'))
    await _until(lambda: target.started == ["EvSlowFirst"])
    second = asyncio.ensure_future(_post(endpoint, '{"id":"EvSlowSecond","text":"x"}'))
    queued = asyncio.ensure_future(_post(endpoint, '{"id":"EvQueued","text":"x"}'))
    await _until(lambda: len(timers.pending) == 3)
    # A request waiting behind an earlier slot answers as soon as its own slot expires.
    timers.fire(2)
    assert await queued == 503
    assert not first.done() and not second.done()
    timers.fire(0)
    assert await first == 503
    await _until(lambda: target.started == ["EvSlowFirst", "EvSlowSecond"])
    timers.fire(1)
    assert await second == 503
    assert await _post(endpoint, '{"id":"EvAfter","text":"x"}') == 200
    target.release.set()
    await _until(lambda: target.delivered == ["EvAfter", "EvSlowFirst", "EvSlowSecond"])
    assert target.recorded == ["EvAfter", "EvSlowFirst", "EvSlowSecond"]
    assert "EvQueued" not in target.started
    await _until(lambda: not endpoint._orphaned_tasks)
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running


async def test_a_stalled_slot_answers_503_and_its_late_durable_write_is_refused() -> None:
    timers = ManualTimers()
    endpoint = _endpoint(timers=timers)
    release = asyncio.Event()
    inbox = TriggerInbox(
        InMemoryObjectStore(),
        Scope("p", "live"),
        TriggerInboxKey("sample", "workspace", "itemCreated", "start"),
    )
    delivered: list[str] = []

    class Consumer:
        async def handle_trigger(
            self, context: TriggerContext, event: TriggerEvent[TriggerJSONPayload]
        ) -> None:
            delivered.append(event.id)

    durable = new_durable_trigger_target(inbox, Consumer(), logger=RecordingTriggerLogger())

    class StallingWrapper:
        async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
            if event.id == "EvStall":
                await release.wait()
            await durable.prepare_trigger(context, event)  # type: ignore[attr-defined]

        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
            await durable.handle_trigger(context, event)

    cancellation = TriggerCancellation()
    running = asyncio.ensure_future(
        endpoint.new_source().run(TriggerContext(cancellation), StallingWrapper())
    )
    await _until(lambda: endpoint.running_source_count() == 1)
    stalled = asyncio.ensure_future(_post(endpoint, '{"id":"EvStall","text":"x"}'))
    await _until(lambda: len(timers.pending) == 1)
    timers.fire(0)
    assert await stalled == 503
    assert await _post(endpoint, '{"id":"EvNext","text":"x"}') == 200
    release.set()
    await _until(lambda: delivered == ["EvNext"] and not endpoint._orphaned_tasks)
    assert await inbox.pending() == ()
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running


async def test_orphaned_failures_are_retrieved_and_a_real_timer_expires_slots() -> None:
    loop = asyncio.get_running_loop()
    unhandled: list[dict[str, Any]] = []
    loop.set_exception_handler(lambda current, context: unhandled.append(context))
    endpoint = _endpoint(arrival_timeout=timedelta(milliseconds=100))
    release = asyncio.Event()

    class FailingLate:
        async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
            await release.wait()
            raise OSError("late failure")

        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
            return None

    cancellation = TriggerCancellation()
    running = asyncio.ensure_future(endpoint.new_source().run(TriggerContext(cancellation), FailingLate()))
    await _until(lambda: endpoint.running_source_count() == 1)
    started = loop.time()
    assert await _post(endpoint, '{"id":"Ev1","text":"x"}') == 503
    assert loop.time() - started < 2
    release.set()
    await _until(lambda: not endpoint._orphaned_tasks)
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running
    import gc

    gc.collect()
    assert unhandled == []
    loop.set_exception_handler(None)


async def test_recording_failures_and_full_queues_answer_503() -> None:
    class FailingPrepare:
        async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
            raise OSError("inbox unavailable")

        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
            return None

    endpoint = _endpoint()
    cancellation = TriggerCancellation()
    running = asyncio.ensure_future(endpoint.new_source().run(TriggerContext(cancellation), FailingPrepare()))
    await _until(lambda: endpoint.running_source_count() == 1)
    response = await endpoint.handle_request("POST", "/h", [], b'{"id":"Ev1","text":"x"}')
    assert (response.status, response.body) == (503, b"webhook delivery temporarily unavailable\n")
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running
    full = _endpoint(source_queue_capacity=1)
    target = GatedTarget()
    full_cancellation = TriggerCancellation()
    full_running = asyncio.ensure_future(full.new_source().run(TriggerContext(full_cancellation), target))
    await _until(lambda: full.running_source_count() == 1)
    assert await _post(full, '{"id":"Ev1","text":"x"}') == 200
    await _until(lambda: target.prepared == ["Ev1"])
    assert await _post(full, '{"id":"Ev2","text":"x"}') == 200
    assert await _post(full, '{"id":"Ev3","text":"x"}') == 503
    full_cancellation.cancel()
    target.gate.set()
    with pytest.raises(TriggerDeliveryAbortedError):
        await full_running


class FakeASGIConnection:
    """Feeds scripted receive messages, then waits for a disconnect the test releases."""

    def __init__(self, messages: list[dict[str, Any]]) -> None:
        self.messages = list(messages)
        self.sent: list[MutableMapping[str, Any]] = []
        self.disconnect = asyncio.Event()

    async def receive(self) -> MutableMapping[str, Any]:
        if self.messages:
            return self.messages.pop(0)
        await self.disconnect.wait()
        return {"type": "http.disconnect"}

    async def send(self, message: MutableMapping[str, Any]) -> None:
        self.sent.append(message)

    def status(self) -> int | None:
        return next(
            (message["status"] for message in self.sent if message["type"] == "http.response.start"), None
        )

    def body(self) -> bytes:
        return b"".join(
            message.get("body", b"") for message in self.sent if message["type"] == "http.response.body"
        )


def _http_scope(method: str = "POST", raw_path: bytes = b"/hooks", query: bytes = b"") -> dict[str, Any]:
    return {
        "type": "http",
        "method": method,
        "path": raw_path.decode(),
        "raw_path": raw_path,
        "query_string": query,
        "root_path": "/mounted",
        "headers": [(b"X-Signature", b"abc"), (b"x-signature", b"second")],
    }


async def test_asgi_reads_chunked_bodies_and_passes_the_raw_request_target() -> None:
    seen: list[WebhookRequest] = []

    async def capture(
        request: WebhookRequest, credentials: dict[str, str], cancellation: TriggerCancellation
    ) -> None:
        seen.append(request)

    endpoint = _endpoint(capture)
    for query, expected in ((b"", "/hooks/a%2Fb"), (b"x=%20", "/hooks/a%2Fb?x=%20")):
        connection = FakeASGIConnection(
            [
                {"type": "http.request", "body": b'{"text":', "more_body": True},
                {"type": "http.request", "body": b'"ignored"}', "more_body": False},
            ]
        )
        await endpoint(
            _http_scope(raw_path=b"/hooks/a%2Fb", query=query), connection.receive, connection.send
        )
        assert connection.status() == 200
        assert seen[-1].url == expected
    assert seen[0].body == b'{"text":"ignored"}'
    assert seen[0].header("X-SIGNATURE") == "abc"
    assert seen[0].header_values("x-signature") == ("abc", "second")
    get = FakeASGIConnection([])
    await endpoint(_http_scope("GET"), get.receive, get.send)
    assert get.status() == 405
    large = FakeASGIConnection([{"type": "http.request", "body": b"x" * 65}])
    await endpoint(_http_scope(), large.receive, large.send)
    assert large.status() == 413
    assert (b"connection", b"close") in large.sent[0]["headers"]
    for scope_type in ("lifespan", "websocket"):
        with pytest.raises(ValueError, match="unsupported ASGI scope type"):
            await endpoint({"type": scope_type}, get.receive, get.send)


async def test_asgi_never_verifies_a_partial_body() -> None:
    verified: list[bytes] = []

    async def spy(
        request: WebhookRequest, credentials: dict[str, str], cancellation: TriggerCancellation
    ) -> None:
        verified.append(request.body)

    endpoint = _endpoint(spy)
    connection = FakeASGIConnection(
        [
            {"type": "http.request", "body": b'{"id":"Ev1"', "more_body": True},
            {"type": "http.disconnect"},
        ]
    )
    await endpoint(_http_scope(), connection.receive, connection.send)
    assert (connection.status(), connection.body()) == (400, b"invalid request body\n")
    assert verified == []


async def test_asgi_disconnect_cancels_verification() -> None:
    is_waiting = asyncio.Event()
    cancellations: list[TriggerCancellation] = []

    async def slow_secret(
        request: WebhookRequest, credentials: dict[str, str], cancellation: TriggerCancellation
    ) -> None:
        cancellations.append(cancellation)
        is_waiting.set()
        await cancellation.wait()
        raise WebhookVerificationUnavailableError()

    endpoint = _endpoint(slow_secret)
    connection = FakeASGIConnection([{"type": "http.request", "body": b'{"id":"Ev1","text":"x"}'}])
    serving = asyncio.ensure_future(endpoint(_http_scope(), connection.receive, connection.send))
    await is_waiting.wait()
    connection.disconnect.set()
    await asyncio.wait_for(serving, 5)
    assert cancellations[0].is_cancelled
    assert connection.sent == []


async def test_lifespan_runs_the_runners_on_the_server_loop() -> None:
    endpoint = _endpoint()
    definition = TriggerDefinition(TriggerRef("sample", "itemCreated"), "Receive an item.")
    delivered: list[str] = []

    class Consumer:
        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
            delivered.append(event.id)

    runner = new_trigger(
        TriggerConfig(
            definition,
            TriggerBindingRef(CONNECTION, definition.trigger, "start"),
            endpoint.new_source(),
            Consumer(),
        )
    )
    application = WebhookEndpointRunner(endpoint, [runner])
    lifespan_messages: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
    sent: list[dict[str, Any]] = []

    async def send(message: MutableMapping[str, Any]) -> None:
        sent.append(dict(message))

    lifespan = asyncio.ensure_future(application({"type": "lifespan"}, lifespan_messages.get, send))
    await lifespan_messages.put({"type": "lifespan.startup"})
    await _until(lambda: endpoint.running_source_count() == 1)
    assert sent == [{"type": "lifespan.startup.complete"}]
    connection = FakeASGIConnection([{"type": "http.request", "body": b'{"id":"Ev1","text":"x"}'}])
    await application(_http_scope(), connection.receive, connection.send)
    assert connection.status() == 200
    await _until(lambda: delivered == ["Ev1"])
    await lifespan_messages.put({"type": "lifespan.shutdown"})
    await asyncio.wait_for(lifespan, 5)
    assert sent[-1] == {"type": "lifespan.shutdown.complete"}
    assert endpoint.running_source_count() == 0
    with pytest.raises(ValueError, match="unsupported ASGI scope type"):
        await application({"type": "websocket"}, connection.receive, connection.send)


async def test_relayed_verification_maps_broker_failures() -> None:
    outcomes: list[BaseException | None] = [
        RelaySignatureInvalidError(),
        CredentialBrokerUnavailableError("x"),
        RelayTokenRenewalDeferredError(),
        None,
    ]

    class Verifier:
        async def verify_relayed_request(self, headers: RelayHeaders, body: bytes, received_at: Any) -> None:
            outcome = outcomes.pop(0)
            if outcome is not None:
                raise outcome

    verify = new_relayed_request_verification(Verifier())
    request = WebhookRequest("/", (), b"")
    with pytest.raises(RelaySignatureInvalidError):
        await verify(request, None, TriggerCancellation())
    for _ in range(2):
        with pytest.raises(WebhookVerificationUnavailableError):
            await verify(request, None, TriggerCancellation())
    await verify(request, None, TriggerCancellation())


class FailingSource:
    async def run(self, context: TriggerContext, target: TriggerTarget[Payload]) -> None:
        raise RuntimeError("source failed")


class WaitingSource:
    def __init__(self) -> None:
        self.is_stopped = False

    async def run(self, context: TriggerContext, target: TriggerTarget[Payload]) -> None:
        await context.cancellation.wait()
        self.is_stopped = True
        raise TriggerDeliveryAbortedError()


class NoOpTarget:
    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
        return None


def _runner(source: Any, name: str) -> Any:
    definition = TriggerDefinition(TriggerRef("sample", "itemCreated"), "Receive an item.")
    return new_trigger(
        TriggerConfig(
            definition, TriggerBindingRef(CONNECTION, definition.trigger, name), source, NoOpTarget()
        )
    )


async def test_endpoint_runner_stops_the_others_when_one_fails_and_reports_aborts() -> None:
    waiting = WaitingSource()
    with pytest.raises(RuntimeError, match="source failed"):
        await WebhookEndpointRunner(_endpoint(), [_runner(FailingSource(), "a"), _runner(waiting, "b")]).run()
    assert waiting.is_stopped
    cancellation = TriggerCancellation()
    stopped = WaitingSource()
    running = asyncio.ensure_future(
        WebhookEndpointRunner(_endpoint(), [_runner(stopped, "c")]).run(TriggerContext(cancellation))
    )
    await asyncio.sleep(0)
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running
    assert stopped.is_stopped
    task_cancelled = WaitingSource()
    cancelled_run = asyncio.ensure_future(
        WebhookEndpointRunner(_endpoint(), [_runner(task_cancelled, "d")]).run()
    )
    await asyncio.sleep(0)
    cancelled_run.cancel()
    with pytest.raises(asyncio.CancelledError):
        await cancelled_run
    with pytest.raises(ValueError):
        WebhookEndpointRunner(_endpoint(), [])


class GatedPrepareTarget:
    """Holds the preparation of one event until its gate opens."""

    def __init__(self, held_event_id: str) -> None:
        self.held_event_id = held_event_id
        self.gate = asyncio.Event()
        self.order: list[str] = []

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
        self.order.append("start " + event.id)
        if event.id == self.held_event_id:
            await self.gate.wait()
        self.order.append("done " + event.id)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Payload]) -> None:
        return None


async def test_a_cancelled_waiting_request_keeps_arrival_order_and_the_earlier_answer() -> None:
    endpoint = _endpoint()
    target = GatedPrepareTarget("Ev1")
    cancellation = TriggerCancellation()
    running = asyncio.ensure_future(endpoint.new_source().run(TriggerContext(cancellation), target))
    await _until(lambda: endpoint.running_source_count() == 1)
    first = asyncio.ensure_future(_post(endpoint, '{"id":"Ev1","text":"a"}'))
    await _until(lambda: target.order == ["start Ev1"])
    second = asyncio.ensure_future(_post(endpoint, '{"id":"Ev2","text":"b"}'))
    await asyncio.sleep(0.01)
    second.cancel()
    with pytest.raises(asyncio.CancelledError):
        await second
    third = asyncio.ensure_future(_post(endpoint, '{"id":"Ev3","text":"c"}'))
    await asyncio.sleep(0.01)
    assert target.order == ["start Ev1"]
    target.gate.set()
    assert (await first, await third) == (200, 200)
    assert target.order == ["start Ev1", "done Ev1", "start Ev3", "done Ev3"]
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running


async def test_answers_503_until_every_created_source_runs() -> None:
    endpoint = _endpoint()
    first_target, second_target = GatedTarget(), GatedTarget()
    cancellation = TriggerCancellation()
    first_source, second_source = endpoint.new_source(), endpoint.new_source()
    first = asyncio.ensure_future(first_source.run(TriggerContext(cancellation), first_target))
    await _until(lambda: endpoint.running_source_count() == 1)
    assert await _post(endpoint, '{"id":"Ev1","text":"a"}') == 503
    assert first_target.prepared == []
    second = asyncio.ensure_future(second_source.run(TriggerContext(cancellation), second_target))
    await _until(lambda: endpoint.running_source_count() == 2)
    assert await _post(endpoint, '{"id":"Ev1","text":"a"}') == 200
    assert (first_target.prepared, second_target.prepared) == (["Ev1"], ["Ev1"])
    first_target.gate.set()
    second_target.gate.set()
    await _until(lambda: first_target.delivered == second_target.delivered == ["Ev1"])
    cancellation.cancel()
    for running in (first, second):
        with pytest.raises(TriggerDeliveryAbortedError):
            await running


async def test_async_decoders_are_awaited() -> None:
    async def decode_later(request: WebhookRequest) -> TriggerEvent[Payload] | None:
        await asyncio.sleep(0)
        return _decode(request)

    endpoint = _endpoint(decode_event=decode_later)
    target = GatedTarget()
    cancellation = TriggerCancellation()
    running = asyncio.ensure_future(endpoint.new_source().run(TriggerContext(cancellation), target))
    await _until(lambda: endpoint.running_source_count() == 1)
    assert await _post(endpoint, '{"id":"Ev1","text":"a"}') == 200
    assert await _post(endpoint, '{"text":"ignored"}') == 200
    assert await _post(endpoint, '{"text":"no id"}') == 400
    assert target.prepared == ["Ev1"]
    target.gate.set()
    await _until(lambda: target.delivered == ["Ev1"])
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running


async def test_lifespan_restarts_failed_runners_after_a_backoff() -> None:
    endpoint = _endpoint()
    source = endpoint.new_source()
    attempts: list[int] = []

    class FlakyStartSource:
        async def run(self, context: TriggerContext, target: TriggerTarget[Payload]) -> None:
            attempts.append(len(attempts) + 1)
            if len(attempts) == 1:
                raise RuntimeError("inbox read failed")
            await source.run(context, target)

    application = WebhookEndpointRunner(endpoint, [_runner(FlakyStartSource(), "start")])
    lifespan_messages: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
    sent: list[dict[str, Any]] = []

    async def send(message: MutableMapping[str, Any]) -> None:
        sent.append(dict(message))

    lifespan = asyncio.ensure_future(application({"type": "lifespan"}, lifespan_messages.get, send))
    await lifespan_messages.put({"type": "lifespan.startup"})
    await _until(lambda: application.run_failure is not None)
    assert str(application.run_failure) == "inbox read failed"
    refused = FakeASGIConnection([{"type": "http.request", "body": b'{"id":"Ev1","text":"x"}'}])
    await application(_http_scope(), refused.receive, refused.send)
    assert refused.status() == 503
    await _until(lambda: endpoint.running_source_count() == 1)
    assert application.run_failure is None
    accepted = FakeASGIConnection([{"type": "http.request", "body": b'{"id":"Ev1","text":"x"}'}])
    await application(_http_scope(), accepted.receive, accepted.send)
    assert (accepted.status(), attempts) == (200, [1, 2])
    assert "webhook endpoint runner failed" in endpoint._config.logger.messages()  # type: ignore[union-attr]
    await lifespan_messages.put({"type": "lifespan.shutdown"})
    await asyncio.wait_for(lifespan, 5)
    assert sent == [{"type": "lifespan.startup.complete"}, {"type": "lifespan.shutdown.complete"}]
