# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Trigger identities, runners, and the Dex Flow and RPC Trigger targets.

Type aliases:

| Name | Meaning |
|---|---|
| `TriggerFilter` | Decides whether an application accepts an event; deterministic and side-effect free. |
| `FlowIdResolver` | Maps an event to the stable application Flow ID that owns it. |
| `FlowInputMapper` | Maps an event to a Flow's start input. |
| `RPCInputMapper` | Maps an event to an application RPC's input. |
"""

import base64
import hashlib
import inspect
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any, Final, Generic, Protocol, TypeVar, cast, overload

import dex

from .configuration_ui import ConnectorConfigurationUI
from .trigger_delivery import (
    UNDELIVERABLE_TRIGGER_ERROR_TYPE,
    UNDELIVERABLE_TRIGGER_MESSAGE,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerDeliveryReplayer,
    TriggerEvent,
    TriggerLogger,
    TriggerLogLevel,
    TriggerLogValue,
    TriggerTarget,
    UndeliverableTriggerError,
    bind_trigger_log_attributes,
    is_trigger_undeliverable,
    log_trigger_record,
    mark_trigger_undeliverable,
    prepare_trigger_delivery,
    trigger_error_attributes,
)
from .types import CONNECTOR_ID_PATTERN, OPERATION_ID_PATTERN, ConnectionRef

P = TypeVar("P")
P_co = TypeVar("P_co", covariant=True)
InputT = TypeVar("InputT")

_MAXIMUM_CAUSE_DEPTH = 32

TRIGGER_RPC_REQUEST_ID_PREFIX: Final = "trigger-rpc-"
"""Prefix of every request ID that `new_dex_rpc_trigger_target` derives."""

_INVALID_ARGUMENT_STATUS_NAME = "INVALID_ARGUMENT"


@dataclass(frozen=True, slots=True)
class TriggerRef:
    """Stable manifest identity of one connector Trigger."""

    connector_id: str
    """DNS-like manifest connector ID."""
    trigger_name: str
    """Lower camel case manifest Trigger name."""

    def validate(self) -> None:
        """Require a DNS-like connector ID and a lower camel case Trigger name; raises ValueError."""
        if not CONNECTOR_ID_PATTERN.fullmatch(self.connector_id):
            raise ValueError("connector ID must be DNS-like and 2-63 characters")
        if not OPERATION_ID_PATTERN.fullmatch(self.trigger_name):
            raise ValueError("trigger name must be lower camel case")

    def to_wire(self) -> dict[str, str]:
        """Encode the Go-compatible Trigger reference."""
        self.validate()
        return {"connectorId": self.connector_id, "triggerName": self.trigger_name}


@dataclass(frozen=True, slots=True)
class TriggerDefinition:
    """One provider event source declared by a connector manifest."""

    trigger: TriggerRef
    """Trigger identity."""
    description: str
    """Application-facing description; must contain a non-blank character."""

    def validate(self) -> None:
        """Validate the identity and require a description; raises ValueError."""
        self.trigger.validate()
        if not self.description.strip():
            raise ValueError("trigger description is required")


@dataclass(frozen=True, slots=True)
class TriggerBindingRef:
    """One configured use of a connector Trigger: its connection, Trigger, and stable binding name."""

    connection: ConnectionRef
    """Provider connection the Trigger reads."""
    trigger: TriggerRef
    """Trigger identity."""
    name: str
    """Stable application binding name."""

    def validate(self) -> None:
        """Validate every identity part; raises ValueError."""
        self.connection.validate()
        self.trigger.validate()
        if not self.name.strip():
            raise ValueError("trigger binding name is required")


@dataclass(frozen=True, slots=True)
class TriggerBindingDefinition:
    """One static application binding exposed to Flow Definition tooling and Dex Web."""

    definition: TriggerDefinition
    """Immutable Trigger definition."""
    connection_name: str
    """Configured provider connection name."""
    binding_name: str
    """Configured Trigger binding name."""
    configuration_ui: ConnectorConfigurationUI | None = None
    """Optional Connector Studio configuration units."""

    def validate(self) -> None:
        """Validate the definition, names, and configuration units; raises ValueError."""
        self.definition.validate()
        if not self.connection_name.strip():
            raise ValueError("trigger binding connection name is required")
        if not self.binding_name.strip():
            raise ValueError("trigger binding name is required")
        if self.configuration_ui is not None:
            self.configuration_ui.validate()


def must_trigger_binding_definition(definition: TriggerBindingDefinition) -> TriggerBindingDefinition:
    """Validate static binding metadata and return it; raises ValueError on invalid wiring."""
    definition.validate()
    return definition


TriggerFilter = Callable[[TriggerEvent[P]], bool]
"""Decides whether an application accepts an event; False consumes it without routing.

It must be deterministic and side-effect free, because durable delivery may evaluate it again after a
restart.
"""

FlowIdResolver = Callable[[TriggerEvent[P]], str]
"""Maps one provider event to the stable application Flow ID that owns it."""

FlowInputMapper = Callable[[TriggerEvent[P]], InputT]
"""Maps one provider event to a Flow's start input."""

RPCInputMapper = Callable[[TriggerEvent[P]], InputT]
"""Maps one provider event to an application RPC's input."""


class TriggerSource(Protocol[P_co]):
    """Owns the provider transport and emits acknowledged events.

    `run` calls `prepare_trigger_delivery` before acknowledging an event, retries a retryable error, and
    treats UndeliverableTriggerError as consumed. It returns or raises only when the context is cancelled
    or it cannot recover.
    """

    async def run(self, context: TriggerContext, target: TriggerTarget[P_co]) -> None:
        """Process provider events until the context is cancelled or an unrecoverable error."""
        ...


class TriggerRunner(Protocol):
    """A configured long-running connector Trigger."""

    @property
    def definition(self) -> TriggerDefinition:
        """Immutable connector Trigger definition."""
        ...

    @property
    def binding(self) -> TriggerBindingRef:
        """Configured Trigger binding identity."""
        ...

    async def run(self, context: TriggerContext | None = None) -> None:
        """Process provider events until the context is cancelled or an unrecoverable error."""
        ...


@dataclass(frozen=True, slots=True)
class TriggerConfig(Generic[P]):
    """One provider source and application target."""

    definition: TriggerDefinition
    """Immutable Trigger definition."""
    binding: TriggerBindingRef
    """Configured binding identity; its Trigger must equal the definition's."""
    source: TriggerSource[P]
    """Polls or subscribes to provider events."""
    target: TriggerTarget[P]
    """Routes accepted events into the application."""
    logger: TriggerLogger | None = None
    """Receives the runner's records; None uses the standard logging adapter."""


class _UndeliverableConsumingTarget(Generic[P]):
    def __init__(self, target: TriggerTarget[P], logger: TriggerLogger) -> None:
        self._target = target
        self._logger = logger

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[P]) -> None:
        try:
            await self._target.handle_trigger(context, event)
        except Exception as error:
            if not is_trigger_undeliverable(error):
                raise
            log_trigger_record(
                self._logger,
                context,
                TriggerLogLevel.WARN,
                "trigger event skipped: undeliverable",
                {"event_id": event.id, **trigger_error_attributes(error)},
            )
            context.record_skip()

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[P]) -> None:
        await prepare_trigger_delivery(context, self._target, event)


class _ConfiguredTriggerRunner(Generic[P]):
    def __init__(self, config: TriggerConfig[P], logger: TriggerLogger) -> None:
        self._config = config
        self._logger = logger

    @property
    def definition(self) -> TriggerDefinition:
        return self._config.definition

    @property
    def binding(self) -> TriggerBindingRef:
        return self._config.binding

    async def run(self, context: TriggerContext | None = None) -> None:
        delivery_context = context if context is not None else TriggerContext()
        target = self._config.target
        if isinstance(target, TriggerDeliveryReplayer):
            try:
                await target.replay_trigger_deliveries(delivery_context)
            except Exception as error:
                raise RuntimeError(f"replay Trigger deliveries: {error}") from error
        await self._config.source.run(delivery_context, _UndeliverableConsumingTarget(target, self._logger))


def new_trigger(config: TriggerConfig[P]) -> TriggerRunner:
    """Validate and construct one Trigger runner.

    `run` replays a replaying target first, then runs the source with a target that consumes
    UndeliverableTriggerError results, logging each with the binding identity, and forwards
    `prepare_trigger`. A replay failure, an abort included, is raised as RuntimeError("replay Trigger
    deliveries: ...") from the original; `asyncio.CancelledError` propagates unchanged. Raises ValueError
    when the binding does not match the definition or the source or target is missing.
    """
    config.definition.validate()
    config.binding.validate()
    if config.binding.trigger != config.definition.trigger:
        raise ValueError("trigger binding does not match its definition")
    if config.source is None or config.target is None:
        raise ValueError("trigger source and target are required")
    binding = config.binding
    logger = bind_trigger_log_attributes(
        config.logger,
        {
            "connector": binding.trigger.connector_id,
            "connection": binding.connection.name,
            "trigger": binding.trigger.trigger_name,
            "binding": binding.name,
        },
    )
    return _ConfiguredTriggerRunner(config, logger)


def _is_trigger_delivery_aborted(error: BaseException | None) -> bool:
    current = error
    for _ in range(_MAXIMUM_CAUSE_DEPTH):
        if current is None:
            return False
        if isinstance(current, TriggerDeliveryAbortedError):
            return True
        current = current.__cause__
    return False


def _is_blank(value: str | None) -> bool:
    return value is None or not value.strip()


class _DexFlowTriggerTarget(Generic[P, InputT]):
    def __init__(
        self,
        client: dex.AsyncClient,
        flow: dex.Flow[InputT],
        accepts_event: TriggerFilter[P],
        resolve_flow_id: FlowIdResolver[P],
        map_to_flow_input: FlowInputMapper[P, InputT],
        logger: TriggerLogger,
    ) -> None:
        self._client = client
        self._flow = flow
        self._accepts_event = accepts_event
        self._resolve_flow_id = resolve_flow_id
        self._map_to_flow_input = map_to_flow_input
        self._logger = logger

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[P]) -> None:
        if not self._accepts_event(event):
            log_trigger_record(
                self._logger,
                context,
                TriggerLogLevel.INFO,
                "trigger event skipped: filtered",
                {"event_id": event.id},
            )
            return
        flow_id = self._resolve_flow_id(event)
        if _is_blank(flow_id) or _is_blank(event.id):
            raise UndeliverableTriggerError(ValueError("Flow trigger requires Flow ID and event ID"))
        flow_input = self._map_to_flow_input(event)
        options = dex.StartFlowOptions(id_reuse_policy=dex.IdReusePolicy.DISALLOW, request_id=event.id)
        try:
            await self._client.start_flow(self._flow, flow_id, flow_input, options)
            is_duplicate = False
        except dex.FlowAlreadyStartedError:
            is_duplicate = True
        except Exception as error:
            raise classify_dex_trigger_error(error)  # noqa: B904
        log_trigger_record(
            self._logger,
            context,
            TriggerLogLevel.DEBUG,
            "trigger event delivered",
            {"event_id": event.id, "flow_id": flow_id, "duplicate": is_duplicate},
        )


def new_dex_flow_trigger_target(
    client: dex.AsyncClient,
    flow: dex.Flow[InputT],
    *,
    accepts_event: TriggerFilter[P],
    resolve_flow_id: FlowIdResolver[P],
    map_to_flow_input: FlowInputMapper[P, InputT],
    logger: TriggerLogger | None = None,
) -> TriggerTarget[P]:
    """Create a filtered target that starts a Dex Flow.

    The event ID is the Flow-start request ID and Flow ID reuse is disallowed, so a duplicate start is
    delivered even after the original Flow completes. A blank Flow ID or event ID, an input that cannot
    be encoded, or a missing Flow raises UndeliverableTriggerError. Mapper errors and every other error
    propagate for a retry. It logs INFO "trigger event skipped: filtered" and DEBUG "trigger event
    delivered" with `duplicate`. Only `dex.AsyncClient` is supported. Raises ValueError when an argument
    is missing.
    """
    if (
        client is None
        or flow is None
        or accepts_event is None
        or resolve_flow_id is None
        or map_to_flow_input is None
    ):
        raise ValueError(
            "Dex client, Flow, Trigger filter, Flow ID resolver, and Flow input mapper are required"
        )
    return _DexFlowTriggerTarget(
        client,
        flow,
        accepts_event,
        resolve_flow_id,
        map_to_flow_input,
        bind_trigger_log_attributes(logger, {"target": "flow_start"}),
    )


def _declares_rpc_input(rpc_method: Callable[..., Any]) -> bool:
    try:
        parameters = list(inspect.signature(rpc_method).parameters.values())
    except (TypeError, ValueError):
        return False
    if not inspect.ismethod(rpc_method) and parameters and parameters[0].name == "self":
        parameters = parameters[1:]
    return len(parameters) >= 2


def _restore_trigger_rpc_request_conflict_error(
    event_id: str, flow_id: str, rpc_name: str, request_id: str, cause: str | None
) -> "TriggerRPCRequestConflictError":
    return TriggerRPCRequestConflictError(event_id, flow_id, rpc_name, request_id, cause)


class TriggerRPCRequestConflictError(UndeliverableTriggerError):
    """The undeliverable failure of an RPC Trigger target whose request ID holds a different request.

    Dex Server 0.1.36 and earlier compare the whole RPC request. They reject an attempt that sends a
    different request under a recorded request ID with a `dex.DexServiceError` whose code is
    INVALID_ARGUMENT and whose detail says the request ID "was already used for a different request".
    `new_dex_rpc_trigger_target` raises this error for that rejection, so Trigger delivery drops the
    event instead of retrying it. Those servers also report the conflict while the earlier request is
    still in flight, so when that request later fails without committing, the event is lost.

    The cause is a non-deterministic or changed input mapper, another binding that routes the same event
    to the same RPC on the same Flow, changed RPC options such as the timeout or locks, or Dex payload
    encryption, which encrypts each attempt's input with a new nonce and so makes every repeat a
    different request. Dex Server 0.1.37 and later compare only the request ID and answer such a repeat
    with the first request's reply instead, so this error does not occur with them.

    The message names the event, RPC, Flow, and request IDs, never the input. The DexServiceError is the
    `cause` and `__cause__`. Instances pickle with their cause reduced to text.
    """

    def __init__(
        self, event_id: str, flow_id: str, rpc_name: str, request_id: str, cause: BaseException | str | None
    ) -> None:
        """Describe the conflict of one delivery attempt.

        `rpc_name` is the RPC name in the request ID: the target's `rpc_name` or the name of the Flow
        attribute that holds the handler. `request_id` is the ID this attempt sent, from
        `trigger_rpc_request_id`. `cause` is the `dex.DexServiceError` that reported the conflict.
        """
        super().__init__(cause)
        self.args = (
            f"{UNDELIVERABLE_TRIGGER_MESSAGE}: RPC trigger event {event_id} for RPC {rpc_name} on Flow "
            f"{flow_id} mapped to a different RPC request than an earlier attempt with request ID "
            f"{request_id} (non-deterministic or changed input mapper, another binding, changed RPC options, "
            "or Dex payload encryption on a Dex Server before 0.1.37); the event was dropped",
        )
        self.event_id = event_id
        """The Trigger event ID."""
        self.flow_id = flow_id
        """The resolved Flow ID."""
        self.rpc_name = rpc_name
        """The RPC name in the request ID."""
        self.request_id = request_id
        """The request ID this attempt sent."""

    def __reduce__(self) -> tuple[Any, ...]:
        """Pickle the identifiers and the cause as text, so arbitrary causes never need to pickle."""
        return (
            _restore_trigger_rpc_request_conflict_error,
            (
                self.event_id,
                self.flow_id,
                self.rpc_name,
                self.request_id,
                None if self.cause is None else str(self.cause),
            ),
        )


def trigger_rpc_request_id(rpc_name: str, event_id: str) -> str:
    """Derive the Dex request ID that `new_dex_rpc_trigger_target` sends on every attempt for one event.

    The ID is `TRIGGER_RPC_REQUEST_ID_PREFIX` followed by the first 16 bytes, as unpadded base64url (22
    characters), of the SHA-256 digest of the UTF-8 bytes of `rpc_name`, one 0x00 byte, and the UTF-8
    bytes of `event_id`. It matches the TypeScript SDK's `triggerRPCRequestId` and the Go SDK's
    `TriggerRPCRequestID` byte for byte. Each surrogate code point, such as one that `surrogateescape`
    decoding or a lone `\\ud800` JSON escape leaves in a string, counts as U+FFFD, as the durable
    Trigger inbox stores it, as TypeScript encodes an unpaired surrogate, and as Go hashes each invalid
    UTF-8 byte, which `surrogateescape` decodes to one surrogate.

    For example, `trigger_rpc_request_id("receiveReply", "Ev1")` hashes the 16 bytes
    `receiveReply\\x00Ev1` and returns `"trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA"`.

    Raises ValueError when `rpc_name` is empty or contains a NUL character.
    """
    if not rpc_name or "\0" in rpc_name:
        raise ValueError("RPC request ID requires an RPC name without NUL characters")
    return _derive_trigger_rpc_request_id(rpc_name, event_id)


def _derive_trigger_rpc_request_id(rpc_name: str, event_id: str) -> str:
    digest = hashlib.sha256(_well_formed_utf8(rpc_name) + b"\0" + _well_formed_utf8(event_id)).digest()
    return TRIGGER_RPC_REQUEST_ID_PREFIX + base64.urlsafe_b64encode(digest[:16]).rstrip(b"=").decode("ascii")


def _well_formed_utf8(text: str) -> bytes:
    if not any(0xD800 <= ord(character) <= 0xDFFF for character in text):
        return text.encode("utf-8")
    return "".join(
        "\ufffd" if 0xD800 <= ord(character) <= 0xDFFF else character for character in text
    ).encode("utf-8")


def _is_trigger_rpc_name_usable(rpc_name: object) -> bool:
    return isinstance(rpc_name, str) and bool(rpc_name.strip()) and "\0" not in rpc_name


def _flow_attribute_name_of(rpc_method: Callable[..., Any]) -> str | None:
    """Find the bound method's first attribute in `dir()` order, as `dex.Registry` names RPCs."""
    receiver = getattr(rpc_method, "__self__", None)
    function = getattr(rpc_method, "__func__", None)
    if receiver is None or function is None:
        return None
    for attribute_name in dir(receiver):
        attribute = inspect.getattr_static(receiver, attribute_name, None)
        is_holding_handler = getattr(attribute, "__func__", attribute) is function
        if is_holding_handler and _is_trigger_rpc_name_usable(attribute_name):
            return attribute_name
    return None


def _is_request_id_used_for_different_request(error: BaseException, request_id: str) -> bool:
    """Match only a conflict whose detail names this call's own request ID."""
    return (
        isinstance(error, dex.DexServiceError)
        and getattr(error.code, "name", None) == _INVALID_ARGUMENT_STATUS_NAME
        and f"request ID {request_id} was already used for a different request" in error.detail
    )


class _DexRPCTriggerTarget(Generic[P]):
    def __init__(
        self,
        client: dex.AsyncClient,
        rpc_method: Callable[..., Any],
        accepts_event: TriggerFilter[P],
        resolve_flow_id: FlowIdResolver[P],
        map_to_rpc_input: RPCInputMapper[P, Any] | None,
        rpc_name: str | None,
        logger: TriggerLogger,
    ) -> None:
        self._client = client
        self._rpc_method = rpc_method
        self._accepts_event = accepts_event
        self._resolve_flow_id = resolve_flow_id
        self._map_to_rpc_input = map_to_rpc_input
        # None sends no request ID.
        self._rpc_name = rpc_name
        self._logger = logger

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[P]) -> None:
        if not self._accepts_event(event):
            log_trigger_record(
                self._logger,
                context,
                TriggerLogLevel.INFO,
                "trigger event skipped: filtered",
                {"event_id": event.id},
            )
            return
        flow_id = self._resolve_flow_id(event)
        if _is_blank(flow_id) or _is_blank(event.id):
            raise UndeliverableTriggerError(ValueError("RPC trigger requires Flow ID and event ID"))
        request_id = (
            None if self._rpc_name is None else _derive_trigger_rpc_request_id(self._rpc_name, event.id)
        )
        request_attributes: dict[str, TriggerLogValue] = (
            {} if request_id is None else {"request_id": request_id}
        )
        invoke_keywords: dict[str, Any] = (
            {} if request_id is None else {"options": dex.RPCInvokeOptions(request_id=request_id)}
        )
        # The Client overloads cannot type a handler chosen at run time.
        rpc_client = cast(Any, self._client)
        try:
            if self._map_to_rpc_input is None:
                await rpc_client.invoke_rpc(self._rpc_method, flow_id, **invoke_keywords)
            else:
                await rpc_client.invoke_rpc(
                    self._rpc_method, flow_id, self._map_to_rpc_input(event), **invoke_keywords
                )
        except dex.ValueMappingError as error:
            if error.operation != "decode":
                raise _classified(error)  # noqa: B904
            log_trigger_record(
                self._logger,
                context,
                TriggerLogLevel.WARN,
                "trigger event delivered; rpc response undecodable",
                {
                    "event_id": event.id,
                    "flow_id": flow_id,
                    **request_attributes,
                    **trigger_error_attributes(error),
                },
            )
            return
        except Exception as error:
            if (
                self._rpc_name is not None
                and request_id is not None
                and _is_request_id_used_for_different_request(error, request_id)
            ):
                log_trigger_record(
                    self._logger,
                    context,
                    TriggerLogLevel.ERROR,
                    "trigger rpc request conflict; event dropped",
                    {
                        "event_id": event.id,
                        "request_id": request_id,
                        "rpc_name": self._rpc_name,
                        "flow_id": flow_id,
                    },
                )
                raise TriggerRPCRequestConflictError(
                    event.id, flow_id, self._rpc_name, request_id, error
                ) from error
            raise _classified(error)  # noqa: B904
        log_trigger_record(
            self._logger,
            context,
            TriggerLogLevel.DEBUG,
            "trigger event delivered",
            {"event_id": event.id, "flow_id": flow_id, **request_attributes},
        )


@overload
def new_dex_rpc_trigger_target(
    client: dex.AsyncClient,
    rpc_method: Callable[[Any, InputT], Any],
    *,
    accepts_event: TriggerFilter[P],
    resolve_flow_id: FlowIdResolver[P],
    map_to_rpc_input: RPCInputMapper[P, InputT],
    rpc_name: str | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerTarget[P]: ...


@overload
def new_dex_rpc_trigger_target(
    client: dex.AsyncClient,
    rpc_method: Callable[[Any], Any],
    *,
    accepts_event: TriggerFilter[P],
    resolve_flow_id: FlowIdResolver[P],
    map_to_rpc_input: None = None,
    rpc_name: str | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerTarget[P]: ...


def new_dex_rpc_trigger_target(
    client: dex.AsyncClient,
    rpc_method: Callable[..., Any],
    *,
    accepts_event: TriggerFilter[P],
    resolve_flow_id: FlowIdResolver[P],
    map_to_rpc_input: RPCInputMapper[P, Any] | None = None,
    rpc_name: str | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerTarget[P]:
    """Create a filtered target that invokes one RPC on the resolved Flow.

    `rpc_method` is the bound `@dex.rpc` method of the registered Flow instance, as
    `AsyncClient.invoke_rpc` accepts it. Without `map_to_rpc_input` the RPC is invoked without input;
    a handler declared with `(ctx, input)` requires a mapper.

    Every delivery attempt of an event sends the same Dex request ID, `trigger_rpc_request_id(rpc_name,
    event.id)`, through `dex.RPCInvokeOptions.request_id`, so Dex Server applies the RPC's Flow changes
    at most once per event while the Flow run keeps that request ID: after a durable inbox replay, a
    provider or relay retry, or a retry after a timeout that hid an applied RPC. The RPC name is
    `rpc_name` when given, and otherwise the name of the Flow attribute that holds the handler, as
    `dex.Registry` names an RPC: its method name, or for a handler that a factory made, the attribute it
    is assigned to. When several attributes hold one handler, the first in `dir()` order is used, as
    the Dex Client does. Each RPC of a Flow therefore gets its own IDs, and they match the TypeScript and
    Go RPC targets' for the same RPC name. Set `rpc_name` to the RPC's wire name when the RPC has an
    `@dex.rpc(name=...)` override; without it, renaming the attribute changes the IDs of events still
    being redelivered. `rpc_name` must contain a non-blank character and no NUL character. Without
    `rpc_name`, a callable that no attribute of its object holds, which the Dex Client rejects as an
    unregistered RPC, is invoked without a request ID.

    A Flow run keeps only its newest request IDs, as many as the Dex Server limit
    `limits.maxRequestIdsPerRun` (1000 by default), shared with its other requests that carry request
    IDs, such as Client Attribute writes; a redelivery after its ID was evicted applies again. Declare the
    handler with `@dex.rpc(is_transactional=True)` or with Attribute locks, so Dex answers a repeat with
    the recorded reply without running the handler again. Any other handler runs again on a repeat, and
    Dex discards that attempt's Flow changes when it makes any. Dex answers a repeat this way also after
    the Flow closed, until it deletes the closed run, so the target consumes such a redelivery as
    delivered. A handler that must also ignore repeats beyond the request-ID window keeps its own record
    of event IDs. The ID covers the RPC name and the event ID only, never the mapped input or the binding,
    so two bindings that route one event to the same RPC on the same Flow share one ID and Dex applies
    only the first; give each such binding its own RPC.

    Keep `map_to_rpc_input` deterministic. Dex Server 0.1.37 and later answer a repeat whose request
    differs, such as one whose mapper returned another input, with the first request's reply and apply
    none of its changes. Earlier servers reject it with INVALID_ARGUMENT "request ID ... was already used
    for a different request", also while the earlier request is still in flight. The target then logs an
    ERROR "trigger rpc request conflict; event dropped" record with the event, request, RPC, and Flow IDs
    and raises `TriggerRPCRequestConflictError`, an UndeliverableTriggerError, so delivery drops the
    event; when that earlier request later fails without committing, the event is lost. Dex payload
    encryption (`dex.ClientOptions.payload_encryption`) encrypts each attempt's input with a new nonce,
    so to those earlier servers every repeat is a different request, even from a deterministic mapper:
    every redelivery of an applied event is dropped as a conflict. Use Dex Server 0.1.37 or later with
    payload encryption.

    It raises UndeliverableTriggerError when the Flow was never started, when the Flow has closed and
    does not hold the event's request ID, when the handler raises UndeliverableTriggerError, when the
    input cannot be encoded, when the Flow ID or event ID is blank, or on a request ID conflict. Python
    and TypeScript handler markers are recognized; Go handler markers are not, and Go targets do not
    recognize Python markers, so a polyglot handler returns success for an event it rejects. A response
    that cannot be decoded after Dex applied the RPC is delivered with a WARN record. Every other error
    propagates for a retry. The DEBUG "trigger event delivered" record, the WARN record, and the conflict
    record carry the `request_id` the target sent. Raises ValueError at construction when an argument is
    missing, a mapper is required, or `rpc_name` is blank or contains NUL. Requires dex-python-sdk 1.45.17
    or later, the first release with `RPCInvokeOptions.request_id`.
    """
    if client is None or rpc_method is None or accepts_event is None or resolve_flow_id is None:
        raise ValueError("Dex client, RPC, Trigger filter, and Flow ID resolver are required")
    if map_to_rpc_input is None and _declares_rpc_input(rpc_method):
        raise ValueError(
            "RPC trigger requires an RPC input mapper for a handler declared with (context, input) parameters"
        )
    if rpc_name is not None and not _is_trigger_rpc_name_usable(rpc_name):
        raise ValueError("RPC trigger rpc_name must contain a non-blank character and no NUL characters")
    if rpc_name is None:
        rpc_name = _flow_attribute_name_of(rpc_method)
    return _DexRPCTriggerTarget(
        client,
        rpc_method,
        accepts_event,
        resolve_flow_id,
        map_to_rpc_input,
        rpc_name,
        bind_trigger_log_attributes(logger, {"target": "rpc"}),
    )


def _classified(error: BaseException) -> BaseException:
    return classify_dex_trigger_error(error)


def _is_handler_undeliverable_marker(error: dex.WorkerInvocationError) -> bool:
    detail = error.worker_error_detail
    return error.worker_error_type == UNDELIVERABLE_TRIGGER_ERROR_TYPE and (
        detail == UNDELIVERABLE_TRIGGER_MESSAGE or detail.startswith(UNDELIVERABLE_TRIGGER_MESSAGE + ": ")
    )


def classify_dex_trigger_error(error: BaseException) -> BaseException:
    """Mark the Dex errors no retry of the same event can fix; return every other error unchanged.

    A closed or never-started Flow, an RPC handler's own UndeliverableTriggerError (reported with type
    `ConnectorsSDK.UndeliverableTriggerError` and the marker message), and an input that cannot be encoded
    are marked. A Worker code alone never qualifies, so lookalike errors stay retryable.
    """
    if is_trigger_undeliverable(error):
        return error
    if isinstance(error, dex.FlowNotActiveOrNotFoundError | dex.FlowNotFoundError):
        return mark_trigger_undeliverable(error)
    if isinstance(error, dex.WorkerInvocationError):
        return mark_trigger_undeliverable(error) if _is_handler_undeliverable_marker(error) else error
    if isinstance(error, dex.ValueMappingError) and error.operation == "encode":
        return mark_trigger_undeliverable(error)
    return error
