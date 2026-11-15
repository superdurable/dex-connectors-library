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

import inspect
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any, Generic, Protocol, TypeVar, cast, overload

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


class _DexRPCTriggerTarget(Generic[P]):
    def __init__(
        self,
        client: dex.AsyncClient,
        rpc_method: Callable[..., Any],
        accepts_event: TriggerFilter[P],
        resolve_flow_id: FlowIdResolver[P],
        map_to_rpc_input: RPCInputMapper[P, Any] | None,
        logger: TriggerLogger,
    ) -> None:
        self._client = client
        self._rpc_method = rpc_method
        self._accepts_event = accepts_event
        self._resolve_flow_id = resolve_flow_id
        self._map_to_rpc_input = map_to_rpc_input
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
        # The Client overloads cannot type a handler chosen at run time.
        rpc_client = cast(Any, self._client)
        try:
            if self._map_to_rpc_input is None:
                await rpc_client.invoke_rpc(self._rpc_method, flow_id)
            else:
                await rpc_client.invoke_rpc(self._rpc_method, flow_id, self._map_to_rpc_input(event))
        except dex.ValueMappingError as error:
            if error.operation != "decode":
                raise _classified(error)  # noqa: B904
            log_trigger_record(
                self._logger,
                context,
                TriggerLogLevel.WARN,
                "trigger event delivered; rpc response undecodable",
                {"event_id": event.id, "flow_id": flow_id, **trigger_error_attributes(error)},
            )
            return
        except Exception as error:
            raise _classified(error)  # noqa: B904
        log_trigger_record(
            self._logger,
            context,
            TriggerLogLevel.DEBUG,
            "trigger event delivered",
            {"event_id": event.id, "flow_id": flow_id},
        )


@overload
def new_dex_rpc_trigger_target(
    client: dex.AsyncClient,
    rpc_method: Callable[[Any, InputT], Any],
    *,
    accepts_event: TriggerFilter[P],
    resolve_flow_id: FlowIdResolver[P],
    map_to_rpc_input: RPCInputMapper[P, InputT],
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
    logger: TriggerLogger | None = None,
) -> TriggerTarget[P]: ...


def new_dex_rpc_trigger_target(
    client: dex.AsyncClient,
    rpc_method: Callable[..., Any],
    *,
    accepts_event: TriggerFilter[P],
    resolve_flow_id: FlowIdResolver[P],
    map_to_rpc_input: RPCInputMapper[P, Any] | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerTarget[P]:
    """Create a filtered target that invokes one RPC on the resolved Flow.

    `rpc_method` is the bound `@dex.rpc` method of the registered Flow instance, as
    `AsyncClient.invoke_rpc` accepts it. Without `map_to_rpc_input` the RPC is invoked without input;
    a handler declared with `(ctx, input)` requires a mapper.

    Delivery is at least once: every attempt sends a new Dex request ID, so a retry after a lost response
    invokes the RPC again, and the handler deduplicates by event ID, as with Go. The TypeScript target's
    stable request ID (sdkts 0.10.1) is not available because dex-python-sdk cannot send one yet.

    It raises UndeliverableTriggerError when the Flow has closed or never started, when the handler
    raises UndeliverableTriggerError, when the input cannot be encoded, or when the Flow ID or event ID
    is blank. Python and TypeScript handler markers are recognized; Go handler markers are not, and Go
    targets do not recognize Python markers, so a polyglot handler returns success for an event it
    rejects. A response that cannot be decoded after Dex applied the RPC is delivered with a WARN record.
    Every other error propagates for a retry. Raises ValueError at construction when an argument is
    missing or a mapper is required.
    """
    if client is None or rpc_method is None or accepts_event is None or resolve_flow_id is None:
        raise ValueError("Dex client, RPC, Trigger filter, and Flow ID resolver are required")
    if map_to_rpc_input is None and _declares_rpc_input(rpc_method):
        raise ValueError(
            "RPC trigger requires an RPC input mapper for a handler declared with (context, input) parameters"
        )
    return _DexRPCTriggerTarget(
        client,
        rpc_method,
        accepts_event,
        resolve_flow_id,
        map_to_rpc_input,
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
