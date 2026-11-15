# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Webhook Trigger endpoints: verify, decode, and durably record provider callbacks in arrival order.

A `WebhookEndpoint` serves one connection and Trigger. It is an ASGI 3 application for the `http` scope
and also offers a framework-neutral `handle_request`. Each Trigger binding runs one source from
`new_source`. `WebhookEndpointRunner` runs the bindings' Trigger runners and, as an ASGI application,
starts them on the server's own event loop through the `lifespan` scope, which is the supported wiring:
sources, inbox locks, and cancellations are bound to one event loop.

Responses are `text/plain; charset=utf-8` and end with a newline: 405 for a method other than POST, 413
for a body over the limit, 503 when credentials cannot be resolved or verification is temporarily
unavailable, 400 for a failed verification or an undecodable event, 503 when the event cannot be
recorded within the arrival timeout, and 200 once every accepting binding recorded it.
"""

import asyncio
import hashlib
import inspect
import logging
import time
import uuid
from collections import deque
from collections.abc import Awaitable, Callable, Iterable, MutableMapping, Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any, Final, Generic, Protocol, TypeAlias, TypeVar, cast

from ..credentials import CredentialProvider, CredentialRefreshDriver, resolve_credential
from ..projectconfig.broker import CredentialBrokerUnavailableError
from ..relaysignature import RelayHeaders
from ..trigger import TriggerFilter, TriggerRunner, _is_trigger_delivery_aborted
from ..trigger_delivery import (
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerLogger,
    TriggerLogLevel,
    TriggerTarget,
    _trigger_retry_delay,
    _wait_for_trigger_delay,
    deliver_trigger,
    log_trigger_record,
    prepare_trigger_delivery,
    trigger_error_attributes,
)
from ..types import CallID, ConnectionRef, OperationRef, new_trigger_call

C = TypeVar("C")
P = TypeVar("P")

DEFAULT_SOURCE_QUEUE_CAPACITY: Final = 64
"""Events one binding's source queues before the endpoint answers 503."""

DEFAULT_ARRIVAL_TIMEOUT: Final = timedelta(seconds=10)
"""How long a verified request may wait to be recorded, measured from when it reserves its slot."""

_RUN_RESTART_RESET: Final = timedelta(seconds=30)

_ASGIScope: TypeAlias = MutableMapping[str, Any]
_ASGIMessage: TypeAlias = MutableMapping[str, Any]
_ASGIReceive: TypeAlias = Callable[[], Awaitable[_ASGIMessage]]
_ASGISend: TypeAlias = Callable[[_ASGIMessage], Awaitable[None]]
_TEXT_HEADERS: Final = (("content-type", "text/plain; charset=utf-8"),)
_LOG = logging.getLogger(__name__)


class _TimerHandle(Protocol):
    def cancel(self) -> None: ...


class WebhookVerificationUnavailableError(Exception):
    """Verification cannot run now, such as an unreadable secret; the endpoint answers 503 for a retry."""

    def __init__(self, cause: BaseException | None = None) -> None:
        """Create the error, optionally caused by cause."""
        super().__init__("webhook verification is temporarily unavailable")
        if cause is not None:
            self.__cause__ = cause


@dataclass(frozen=True, slots=True)
class WebhookRequest:
    """One received webhook request with its complete raw body."""

    url: str
    """The raw request target: path and query exactly as received, without the ASGI root path."""
    headers: tuple[tuple[str, str], ...]
    """Header pairs in arrival order with lower-case names; duplicates are kept."""
    body: bytes = field(repr=False)
    """The complete raw body, as signatures cover it."""
    received_at: datetime = field(default_factory=lambda: datetime.now(UTC))
    """When the endpoint received the request, timezone-aware."""

    def header(self, name: str) -> str | None:
        """Return the first value of a header, matched case-insensitively, or None."""
        wanted = name.lower()
        return next((value for key, value in self.headers if key == wanted), None)

    def header_values(self, name: str) -> tuple[str, ...]:
        """Return every value of a header in arrival order, matched case-insensitively."""
        wanted = name.lower()
        return tuple(value for key, value in self.headers if key == wanted)


@dataclass(frozen=True, slots=True)
class WebhookResponse:
    """The endpoint's answer to one request."""

    status: int
    """HTTP status code."""
    headers: tuple[tuple[str, str], ...] = ()
    """Response header pairs."""
    body: bytes = b""
    """Response body."""


@dataclass(frozen=True, slots=True)
class WebhookEndpointConfig(Generic[C, P]):
    """Static configuration of one connection's webhook endpoint for one Trigger."""

    connector_id: str
    """Manifest connector ID."""
    trigger_name: str
    """Manifest Trigger name."""
    connection: ConnectionRef
    """Connection whose credentials verify requests."""
    credentials: CredentialProvider[C]
    """Resolves the connection's credentials for each request."""
    verify_request: Callable[[WebhookRequest, C, TriggerCancellation], Awaitable[None]]
    """Verifies a request; raise WebhookVerificationUnavailableError for 503, anything else for 400.

    The cancellation fires when the client disconnects.
    """
    decode_event: Callable[[WebhookRequest], "TriggerEvent[P] | None | Awaitable[TriggerEvent[P] | None]"]
    """Decodes a verified request, synchronously or as an awaitable.

    None is a valid event no binding handles (200); an exception is 400.
    """
    maximum_body_bytes: int
    """Largest accepted body in bytes; positive."""
    credential_refresh: CredentialRefreshDriver[C] | None = None
    """Optional refresh driver used to resolve credentials."""
    clock: Callable[[], datetime] | None = None
    """Returns the receipt time; None uses the current UTC time."""
    source_queue_capacity: int = DEFAULT_SOURCE_QUEUE_CAPACITY
    """Events each source queues; at least 1."""
    arrival_timeout: timedelta = DEFAULT_ARRIVAL_TIMEOUT
    """How long a verified request may wait to be recorded; positive."""
    logger: TriggerLogger | None = None
    """Receives the sources' delivery records; None uses the standard logging adapter."""


def _text_response(status: int, text: str, headers: tuple[tuple[str, str], ...] = ()) -> WebhookResponse:
    return WebhookResponse(status, _TEXT_HEADERS + headers, (text + "\n").encode())


def _asgi_request_target(scope: _ASGIScope) -> str:
    raw_path = scope.get("raw_path") or str(scope.get("path", "/")).encode("utf-8")
    query = scope.get("query_string") or b""
    return bytes(raw_path).decode("latin-1") + ("?" + bytes(query).decode("latin-1") if query else "")


class WebhookEndpoint(Generic[C, P]):
    """Verifies, decodes, and records one connection's webhook requests for one Trigger.

    Only a verified, decoded event takes an arrival slot, so unauthorized requests never hold up valid
    ones. Events are recorded one at a time in arrival order: each running source that accepts an event
    prepares it, such as a durable inbox write, and then queues it, before the request is answered 200.
    A slot expires after `arrival_timeout` from its reservation or when the client disconnects; the
    request then answers 503 even while it still waits behind an earlier one. A write already in flight
    at expiry still completes and queues the event, so delivery is at least once.
    """

    def __init__(
        self,
        config: WebhookEndpointConfig[C, P],
        *,
        _schedule: Callable[[float, Callable[[], None]], _TimerHandle] | None = None,
    ) -> None:
        """Validate config; raises ValueError for a blank identity, missing callable, or invalid bound."""
        if not config.connector_id.strip() or not config.trigger_name.strip():
            raise ValueError("webhook endpoint connector ID and Trigger name are required")
        if config.connection is None or not config.connection.name.strip():
            raise ValueError("webhook endpoint connection name is required")
        if config.credentials is None or config.decode_event is None or config.verify_request is None:
            raise ValueError("webhook endpoint credentials, decode_event, and verify_request are required")
        if isinstance(config.maximum_body_bytes, bool) or config.maximum_body_bytes <= 0:
            raise ValueError("webhook endpoint maximum_body_bytes must be a positive integer")
        if isinstance(config.source_queue_capacity, bool) or config.source_queue_capacity <= 0:
            raise ValueError("webhook endpoint source_queue_capacity must be a positive integer")
        if config.arrival_timeout <= timedelta(0):
            raise ValueError("webhook endpoint arrival_timeout must be positive")
        self._config = config
        self._schedule = _schedule
        self._sources: dict[WebhookTriggerSource[P], TriggerTarget[P]] = {}
        self._created_source_count = 0
        self._arrival_tail: asyncio.Future[None] | None = None
        self._orphaned_tasks: set[asyncio.Future[Any]] = set()

    def new_source(self, accepts_event: TriggerFilter[P] | None = None) -> "WebhookTriggerSource[P]":
        """Create one binding's source; accepts_event selects its events and None accepts every event.

        The endpoint answers 503 to verified events until every created source is running, so a binding
        still replaying its inbox never misses an event another binding acknowledged.
        """
        self._created_source_count += 1
        return WebhookTriggerSource(self, accepts_event or (lambda event: True))

    def running_source_count(self) -> int:
        """Return how many sources are running."""
        return len(self._sources)

    async def handle_request(
        self,
        method: str,
        url: str,
        headers: Iterable[tuple[str, str]],
        body: bytes,
        cancellation: TriggerCancellation | None = None,
    ) -> WebhookResponse:
        """Answer one request with its complete body; framework adapters call it.

        url is the raw request target and cancellation should fire when the client disconnects. Never
        pass a partial body: verification and decoding assume the complete bytes.
        """
        if method.upper() != "POST":
            return _text_response(405, "method not allowed", (("allow", "POST"),))
        if len(body) > self._config.maximum_body_bytes:
            return _text_response(413, "request body too large", (("connection", "close"),))
        request_cancellation = cancellation if cancellation is not None else TriggerCancellation()
        received_at = self._config.clock() if self._config.clock is not None else datetime.now(UTC)
        request = WebhookRequest(
            url, tuple((name.lower(), value) for name, value in headers), bytes(body), received_at
        )
        try:
            credentials = await self._resolve_credentials(request.body, request_cancellation)
        except Exception:  # noqa: BLE001
            return _text_response(503, "webhook temporarily unavailable")
        try:
            await self._config.verify_request(request, credentials, request_cancellation)
        except WebhookVerificationUnavailableError:
            return _text_response(503, "webhook temporarily unavailable")
        except Exception:  # noqa: BLE001
            return _text_response(400, "invalid webhook signature")
        try:
            decoded = self._config.decode_event(request)
            event = await decoded if inspect.isawaitable(decoded) else decoded
        except Exception:  # noqa: BLE001
            return _text_response(400, "invalid webhook event")
        if event is None:
            return WebhookResponse(200)
        try:
            await self._record_in_arrival_order(event, request_cancellation)
        except Exception:  # noqa: BLE001
            return _text_response(503, "webhook delivery temporarily unavailable")
        return WebhookResponse(200)

    async def __call__(self, scope: _ASGIScope, receive: _ASGIReceive, send: _ASGISend) -> None:
        """Serve one ASGI 3 `http` request; raises ValueError for every other scope type."""
        if scope.get("type") != "http":
            raise ValueError("unsupported ASGI scope type")
        await _serve_asgi_http(self, scope, receive, send)

    def _activate(self, source: "WebhookTriggerSource[P]", target: TriggerTarget[P]) -> None:
        self._sources[source] = target

    def _deactivate(self, source: "WebhookTriggerSource[P]") -> None:
        self._sources.pop(source, None)

    async def _resolve_credentials(self, body: bytes, cancellation: TriggerCancellation) -> C:
        call = new_trigger_call(
            self._config.connection,
            OperationRef(self._config.connector_id, self._config.trigger_name),
            _credential_call_id(self._config.connector_id, self._config.connection, body),
        )
        if self._config.credential_refresh is not None:
            resolution = resolve_credential(self._config.credentials, call, self._config.credential_refresh)
        else:
            resolution = self._config.credentials.resolve(call)
        return cast(C, await _first_of_or_cancelled(asyncio.ensure_future(resolution), cancellation))

    async def _record_in_arrival_order(
        self, event: TriggerEvent[P], request_cancellation: TriggerCancellation
    ) -> None:
        loop = asyncio.get_running_loop()
        slot = TriggerCancellation(request_cancellation)
        schedule = self._schedule or loop.call_later
        timer = schedule(self._config.arrival_timeout.total_seconds(), slot.cancel)
        previous = self._arrival_tail
        turn_finished: asyncio.Future[None] = loop.create_future()
        self._arrival_tail = turn_finished
        turn = asyncio.ensure_future(self._run_arrival_turn(previous, turn_finished, slot, event))
        try:
            await _first_of_or_cancelled(turn, slot, cancel_on_expiry=False)
        except (TriggerDeliveryAbortedError, asyncio.CancelledError):
            # A cancelled request also gives up its slot; a turn not yet recording then skips.
            slot.cancel()
            if not turn.done():
                self._retain_orphan(turn)
            raise
        finally:
            timer.cancel()

    async def _run_arrival_turn(
        self,
        previous: "asyncio.Future[None] | None",
        turn_finished: "asyncio.Future[None]",
        slot: TriggerCancellation,
        event: TriggerEvent[P],
    ) -> None:
        try:
            if previous is not None:
                # Shielded: cancelling this turn must not cancel the earlier turn's shared future.
                await asyncio.shield(previous)
            slot.raise_if_cancelled()
            record = asyncio.ensure_future(self._record(slot, event))
            try:
                await _first_of_or_cancelled(record, slot, cancel_on_expiry=False)
            except (TriggerDeliveryAbortedError, asyncio.CancelledError):
                if not record.done():
                    self._retain_orphan(record)
                raise
        finally:
            _finish_turn_after(previous, turn_finished)

    async def _record(self, slot: TriggerCancellation, event: TriggerEvent[P]) -> None:
        running = list(self._sources.items())
        if not running:
            raise RuntimeError("no webhook Trigger source is running")
        if len(running) < self._created_source_count:
            raise RuntimeError("a webhook Trigger source is not running yet")
        accepting: list[WebhookTriggerSource[P]] = []
        for source, target in running:
            if not source._accepts(event):
                continue
            await prepare_trigger_delivery(TriggerContext(slot), target, event)
            accepting.append(source)
        for source in accepting:
            source._enqueue(event)

    def _retain_orphan(self, task: "asyncio.Task[Any] | asyncio.Future[Any]") -> None:
        orphan: asyncio.Future[Any] = task
        self._orphaned_tasks.add(orphan)
        orphan.add_done_callback(self._finish_orphan)

    def _finish_orphan(self, task: "asyncio.Future[Any]") -> None:
        self._orphaned_tasks.discard(task)
        if task.cancelled():
            return
        error = task.exception()
        if error is not None:
            log_trigger_record(
                self._config.logger,
                TriggerContext(),
                TriggerLogLevel.DEBUG,
                "webhook late record failed",
                trigger_error_attributes(error),
            )


async def _first_of_or_cancelled(
    work: "asyncio.Future[Any]", cancellation: TriggerCancellation, *, cancel_on_expiry: bool = True
) -> Any:
    """Await work, or raise TriggerDeliveryAbortedError once cancellation fires first."""
    if cancellation.is_cancelled and not work.done():
        if cancel_on_expiry:
            work.cancel()
        raise TriggerDeliveryAbortedError()
    waiting = asyncio.ensure_future(cancellation.wait())
    try:
        await asyncio.wait({work, waiting}, return_when=asyncio.FIRST_COMPLETED)
    except asyncio.CancelledError:
        waiting.cancel()
        if cancel_on_expiry:
            work.cancel()
        raise
    waiting.cancel()
    if work.done():
        return work.result()
    if cancel_on_expiry:
        work.cancel()
        try:
            await work
        except BaseException:  # noqa: BLE001
            # The cancelled work's outcome no longer matters; the request has ended.
            pass
    raise TriggerDeliveryAbortedError()


def _finish_turn_after(
    previous: "asyncio.Future[None] | None", turn_finished: "asyncio.Future[None]"
) -> None:
    """Finish a turn only after the earlier turn, so a cancelled turn never lets a later one overtake."""

    def finish(_: object = None) -> None:
        if not turn_finished.done():
            turn_finished.set_result(None)

    if previous is None or previous.done():
        finish()
    else:
        previous.add_done_callback(finish)


def _credential_call_id(connector_id: str, connection: ConnectionRef, body: bytes) -> CallID:
    digest = hashlib.sha256()
    for part in (connector_id, connection.provider, connection.name):
        digest.update(part.encode("utf-8"))
        digest.update(b"\0")
    digest.update(body)
    identity = bytearray(digest.digest()[:16])
    identity[6] = (identity[6] & 0x0F) | 0x50
    identity[8] = (identity[8] & 0x3F) | 0x80
    return CallID(str(uuid.UUID(bytes=bytes(identity))))


class WebhookTriggerSource(Generic[P]):
    """One binding's source of an endpoint's events; create it with `WebhookEndpoint.new_source`.

    `run` registers the source with its endpoint, delivers queued events in order with `deliver_trigger`,
    and raises TriggerDeliveryAbortedError once the context is cancelled. It runs at most once at a time.
    """

    def __init__(self, endpoint: WebhookEndpoint[Any, P], accepts_event: TriggerFilter[P]) -> None:
        """Bind the source to endpoint; applications call `WebhookEndpoint.new_source` instead."""
        self._endpoint = endpoint
        self._accepts_event = accepts_event
        self._queue: deque[TriggerEvent[P]] = deque()
        self._wake: asyncio.Event | None = None
        self._is_running = False

    def _accepts(self, event: TriggerEvent[P]) -> bool:
        return self._accepts_event(event)

    def _enqueue(self, event: TriggerEvent[P]) -> None:
        if len(self._queue) >= self._endpoint._config.source_queue_capacity:
            raise RuntimeError("webhook Trigger delivery queue is full")
        self._queue.append(event)
        if self._wake is not None:
            self._wake.set()

    async def run(self, context: TriggerContext, target: TriggerTarget[P]) -> None:
        """Deliver this binding's events until the context is cancelled.

        Raises ValueError without a target and RuntimeError when the source is already running.
        """
        if target is None:
            raise ValueError("webhook Trigger target is required")
        if self._is_running:
            raise RuntimeError("webhook Trigger source is already running")
        self._is_running = True
        self._endpoint._activate(self, target)
        try:
            while True:
                context.cancellation.raise_if_cancelled()
                if self._queue:
                    event = self._queue.popleft()
                    await deliver_trigger(context, target, event, logger=self._endpoint._config.logger)
                    continue
                self._wake = asyncio.Event()
                woken = asyncio.ensure_future(self._wake.wait())
                try:
                    await _first_of_or_cancelled(woken, context.cancellation)
                finally:
                    self._wake = None
        finally:
            self._endpoint._deactivate(self)
            self._is_running = False


class RelayedRequestVerifier(Protocol):
    """Verifies that a Dex host signed a relayed request, as `LoadedProject` does."""

    async def verify_relayed_request(self, headers: RelayHeaders, body: bytes, received_at: datetime) -> None:
        """Raise when the request is not validly signed."""
        ...


def new_relayed_request_verification(
    verifier: RelayedRequestVerifier,
) -> Callable[[WebhookRequest, Any, TriggerCancellation], Awaitable[None]]:
    """Return a `verify_request` that checks the Dex relay signature with verifier.

    A CredentialBrokerUnavailableError, including RelayTokenRenewalDeferredError, becomes
    WebhookVerificationUnavailableError (503); every other error is a refusal (400). Raises ValueError
    without a verifier.
    """
    if verifier is None:
        raise ValueError("relayed request verifier is required")

    async def verify_request(
        request: WebhookRequest, credentials: Any, cancellation: TriggerCancellation
    ) -> None:
        try:
            await verifier.verify_relayed_request(request.headers, request.body, request.received_at)
        except CredentialBrokerUnavailableError as error:
            raise WebhookVerificationUnavailableError(error) from error

    return verify_request


class WebhookEndpointRunner:
    """Serves one endpoint and runs the Trigger runners of its bindings together.

    As an ASGI application it serves `http` through the endpoint and starts the runners on the server's
    event loop at `lifespan` startup, stopping them at shutdown; for example `uvicorn.run(runner)`. When
    the runners stop or fail before shutdown, such as on a transient inbox read error during replay, it
    logs the failure, answers `http` with 503, and restarts them after a backoff of 250 milliseconds
    doubling to 30 seconds; `run_failure` reports the latest failure for a readiness probe. A host that
    owns the loop calls `run(context)` itself next to the server and stops it with
    `context.cancellation.cancel()`.
    """

    def __init__(self, endpoint: WebhookEndpoint[Any, Any], runners: Sequence[TriggerRunner]) -> None:
        """Bind the endpoint and at least one runner; raises ValueError otherwise."""
        if endpoint is None or not runners or any(runner is None for runner in runners):
            raise ValueError("webhook endpoint runner requires an endpoint and at least one Trigger runner")
        self._endpoint = endpoint
        self._runners = tuple(runners)
        self._context = TriggerContext()
        self._run_task: asyncio.Task[None] | None = None
        self._is_shutting_down = False
        self._run_failure: BaseException | None = None

    @property
    def run_failure(self) -> BaseException | None:
        """The failure that stopped the lifespan-started runners, or None while they run.

        It is set from the failure until the next restart attempt begins; a host-owned `run` never sets
        it.
        """
        return self._run_failure

    async def handle_request(
        self,
        method: str,
        url: str,
        headers: Iterable[tuple[str, str]],
        body: bytes,
        cancellation: TriggerCancellation | None = None,
    ) -> WebhookResponse:
        """Answer one request through the endpoint; see `WebhookEndpoint.handle_request`."""
        return await self._endpoint.handle_request(method, url, headers, body, cancellation)

    async def __call__(self, scope: _ASGIScope, receive: _ASGIReceive, send: _ASGISend) -> None:
        """Serve ASGI 3 `http` and `lifespan` scopes; raises ValueError for every other scope type."""
        scope_type = scope.get("type")
        if scope_type == "lifespan":
            await self._serve_lifespan(receive, send)
            return
        if scope_type != "http":
            raise ValueError("unsupported ASGI scope type")
        if self._run_failure is not None:
            await _send_response(send, _text_response(503, "webhook delivery temporarily unavailable"))
            return
        await _serve_asgi_http(self._endpoint, scope, receive, send)

    async def run(self, context: TriggerContext | None = None) -> None:
        """Run every Trigger runner until the context is cancelled or one of them stops.

        When the first runner stops, the others are cancelled and awaited. Raises
        TriggerDeliveryAbortedError when context was cancelled, otherwise the first runner's failure
        unless it is an abort. Task cancellation cancels the runners and propagates.
        """
        parent = context if context is not None else TriggerContext()
        child_cancellation = TriggerCancellation(parent.cancellation)
        child = TriggerContext._derive(child_cancellation, parent.log_attributes, parent.record_skip)
        tasks = [asyncio.ensure_future(runner.run(child)) for runner in self._runners]
        try:
            done, _ = await asyncio.wait(tasks, return_when=asyncio.FIRST_COMPLETED)
        except asyncio.CancelledError:
            child_cancellation.cancel()
            for task in tasks:
                task.cancel()
            await asyncio.gather(*tasks, return_exceptions=True)
            raise
        first_failure = next(
            (
                task.exception()
                for task in tasks
                if task in done and not task.cancelled() and task.exception()
            ),
            None,
        )
        child_cancellation.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
        if parent.cancellation.is_cancelled:
            raise TriggerDeliveryAbortedError()
        if first_failure is not None and not _is_trigger_delivery_aborted(first_failure):
            raise first_failure

    async def _serve_lifespan(self, receive: _ASGIReceive, send: _ASGISend) -> None:
        while True:
            message = await receive()
            if message.get("type") == "lifespan.startup":
                self._run_task = asyncio.ensure_future(self._run_until_shutdown())
                await send({"type": "lifespan.startup.complete"})
            elif message.get("type") == "lifespan.shutdown":
                self._is_shutting_down = True
                self._context.cancellation.cancel()
                if self._run_task is not None:
                    try:
                        await self._run_task
                    except TriggerDeliveryAbortedError:
                        pass
                    except Exception as error:  # noqa: BLE001
                        self._log_run_failure(error)
                await send({"type": "lifespan.shutdown.complete"})
                return

    async def _run_until_shutdown(self) -> None:
        failed_attempts = 0
        while True:
            started_at = time.monotonic()
            try:
                await self.run(self._context)
                failure: BaseException = RuntimeError("webhook Trigger runners stopped")
            except TriggerDeliveryAbortedError:
                if self._context.cancellation.is_cancelled:
                    return
                failure = RuntimeError("webhook Trigger runners stopped")
            except Exception as error:  # noqa: BLE001
                failure = error
            if self._is_shutting_down or self._context.cancellation.is_cancelled:
                return
            self._run_failure = failure
            self._log_run_failure(failure)
            # A run that outlived the longest backoff starts the backoff over.
            if time.monotonic() - started_at >= _RUN_RESTART_RESET.total_seconds():
                failed_attempts = 0
            failed_attempts += 1
            try:
                restart_delay = _trigger_retry_delay(failed_attempts)
                await _wait_for_trigger_delay(self._context.cancellation, restart_delay)
            except TriggerDeliveryAbortedError:
                return
            self._run_failure = None

    def _log_run_failure(self, error: BaseException) -> None:
        log_trigger_record(
            self._endpoint._config.logger,
            TriggerContext(),
            TriggerLogLevel.ERROR,
            "webhook endpoint runner failed",
            trigger_error_attributes(error),
        )


async def _send_response(send: _ASGISend, response: WebhookResponse) -> None:
    await send(
        {
            "type": "http.response.start",
            "status": response.status,
            "headers": [
                (name.encode("latin-1"), value.encode("latin-1")) for name, value in response.headers
            ],
        }
    )
    await send({"type": "http.response.body", "body": response.body})


async def _read_asgi_body(receive: _ASGIReceive, maximum_bytes: int) -> bytes | WebhookResponse:
    chunks: list[bytes] = []
    size = 0
    while True:
        message = await receive()
        if message.get("type") != "http.request":
            return _text_response(400, "invalid request body")
        chunk = bytes(message.get("body", b""))
        size += len(chunk)
        if size > maximum_bytes:
            return _text_response(413, "request body too large", (("connection", "close"),))
        chunks.append(chunk)
        if not message.get("more_body", False):
            return b"".join(chunks)


async def _watch_disconnect(receive: _ASGIReceive, cancellation: TriggerCancellation) -> None:
    while True:
        message = await receive()
        if message.get("type") == "http.disconnect":
            cancellation.cancel()
            return


async def _serve_asgi_http(
    endpoint: WebhookEndpoint[Any, Any], scope: _ASGIScope, receive: _ASGIReceive, send: _ASGISend
) -> None:
    method = str(scope.get("method", ""))
    if method.upper() != "POST":
        await _send_response(send, _text_response(405, "method not allowed", (("allow", "POST"),)))
        return
    body = await _read_asgi_body(receive, endpoint._config.maximum_body_bytes)
    if isinstance(body, WebhookResponse):
        try:
            await _send_response(send, body)
        except Exception:  # noqa: BLE001
            # The client is gone, so there is nobody to answer.
            _LOG.debug("webhook response after an incomplete body was not sent")
        return
    headers = [
        (bytes(name).decode("latin-1"), bytes(value).decode("latin-1"))
        for name, value in scope.get("headers", [])
    ]
    cancellation = TriggerCancellation()
    watcher = asyncio.ensure_future(_watch_disconnect(receive, cancellation))
    try:
        response = await endpoint.handle_request(
            method, _asgi_request_target(scope), headers, body, cancellation
        )
        if not cancellation.is_cancelled:
            await _send_response(send, response)
    finally:
        watcher.cancel()


__all__ = [
    "DEFAULT_ARRIVAL_TIMEOUT",
    "DEFAULT_SOURCE_QUEUE_CAPACITY",
    "RelayedRequestVerifier",
    "WebhookEndpoint",
    "WebhookEndpointConfig",
    "WebhookEndpointRunner",
    "WebhookRequest",
    "WebhookResponse",
    "WebhookTriggerSource",
    "WebhookVerificationUnavailableError",
    "new_relayed_request_verification",
]
