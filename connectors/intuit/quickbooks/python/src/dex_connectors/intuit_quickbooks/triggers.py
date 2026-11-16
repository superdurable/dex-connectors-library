# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The QuickBooks invoiceChanged poll Trigger: one Flow per invoice created or changed in the company.

The runners poll the invoice query on the Python SDK's durable poll Trigger kit at parity with the Go
connector. Their cursor documents and Trigger inbox payloads match the Go connector's, so Go and Python
replicas share one binding.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass, field
from datetime import timedelta

import dex
from dex_connectors.sdk import (
    TriggerBindingRef,
    TriggerConfig,
    TriggerLogger,
    TriggerRunner,
    TriggerTarget,
    new_trigger,
)
from dex_connectors.sdk.pollingtrigger import (
    BindingPollerConfig,
    CursorStore,
    PollRunner,
    PollRunnerConfig,
    PollSchedule,
    new_binding_poller,
    new_binding_trigger_source,
    new_memory_cursor_store,
)
from dex_connectors.sdk.projectconfig import (
    LoadedProject,
    ObjectNotFoundError,
    PollCursorKey,
    TriggerInboxKey,
    decode_trigger_configuration,
    new_durable_trigger_target,
)

from ._generated.definitions import CONNECTOR_ID, INVOICE_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_invoice_changed_trigger_configuration,
    define_invoice_changed_trigger_binding,
)
from .client import Client
from .connection import Connection
from .invoice_change_fetcher import (
    INVOICE_CHANGE_CREATED,
    INVOICE_CHANGE_UPDATED,
    InvoiceChangeFetcher,
)
from .models import InvoiceChangedEvent, InvoiceChangedTriggerConfiguration

_TRIGGER_NAME = INVOICE_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


def decode_invoice_changed_event_payload(payload_json: str, payload_bytes: bytes) -> InvoiceChangedEvent:
    """Decode a replayed Trigger inbox payload that the Go or Python connector recorded."""
    decoded = (
        dex.CodecRegistry().resolve(InvoiceChangedEvent).decode(dex.Value(dex.WireKind.JSON, payload_json))
    )
    if not isinstance(decoded, InvoiceChangedEvent):
        raise ValueError("pending QuickBooks invoiceChanged event is invalid")
    return decoded


@dataclass(frozen=True, slots=True)
class InvoiceChangedTriggerRoute:
    """Bind one invoiceChanged binding to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    target: TriggerTarget[InvoiceChangedEvent]
    """Routes each invoice change into the application.

    A durable target from `new_durable_trigger_target` records each change before the cursor commits past
    it; any other target consumes it first.
    """
    configuration: InvoiceChangedTriggerConfiguration = field(
        default_factory=InvoiceChangedTriggerConfiguration
    )
    """The binding configuration; it has no fields today, so a binding watches every invoice."""
    cursor: CursorStore | None = None
    """Stores the binding's watermark, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while stopped
    are lost.
    """


@dataclass(frozen=True, slots=True)
class InvoiceTriggerRunnerConfig:
    """Configures every QuickBooks Trigger binding polled through one connection."""

    connection: Connection
    """A provider connection from `new_connection` or `new_project_connection`; a mock has no client."""
    invoice_changed_routes: Sequence[InvoiceChangedTriggerRoute]
    """The invoiceChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives seeds, reseeds, failed polls, and rate-limit pauses; None uses standard logging.

    Records carry binding identity, never invoice payloads or credentials.
    """


def _connection_client(connection: Connection) -> Client:
    if connection is None:
        raise ValueError("QuickBooks invoiceChanged connection is required")
    client = connection._client
    if client is None:
        raise ValueError("QuickBooks invoiceChanged Triggers require a provider connection, not a mock")
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.config.poll_interval_seconds))


def new_invoice_trigger_runner(config: InvoiceTriggerRunnerConfig) -> PollRunner:
    """Create one poll runner for the configured invoiceChanged bindings.

    `run` replays each durable target, records a baseline for a binding without a cursor, then polls every
    binding each `pollInterval` with jitter, one QuickBooks query of at most 100 invoices per request and at
    most ten pages per poll. Each poll rereads the 30 seconds before the newest change delivered, so a
    write QuickBooks made visible after an earlier poll still arrives. A 429 pauses every binding of the
    connection for Retry-After, or 60 seconds without it; other failures back off per binding. A cursor
    for another company or environment, or a QuickBooks validation Fault for the watermark query, records
    a new baseline and logs a WARN record; changes in that gap are not delivered. Delivery is at least
    once, and each invoice version keeps one Trigger event ID. Raises ValueError for a missing or mock
    connection, no routes, or a blank binding name or missing target.
    """
    client = _connection_client(config.connection)
    if not config.invoice_changed_routes:
        raise ValueError("QuickBooks invoiceChanged routes are required")
    reference = config.connection.reference
    pollers = []
    for route in config.invoice_changed_routes:
        if route is None or not (route.binding_name or "").strip() or route.target is None:
            raise ValueError("QuickBooks invoiceChanged binding name and target are required")
        pollers.append(
            new_binding_poller(
                BindingPollerConfig(
                    definition=INVOICE_CHANGED_TRIGGER_DEFINITION,
                    binding=TriggerBindingRef(
                        reference, INVOICE_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
                    ),
                    cursor=route.cursor if route.cursor is not None else new_memory_cursor_store(),
                    fetcher=InvoiceChangeFetcher(client, reference),
                    target=route.target,
                )
            )
        )
    return PollRunner(PollRunnerConfig(reference, _poll_schedule(client), pollers, logger=config.logger))


@dataclass(frozen=True, slots=True)
class ProjectInvoiceChangedTriggerRoute:
    """Select one invoiceChanged binding saved in the project configuration."""

    binding_name: str
    """The binding name."""
    target: TriggerTarget[InvoiceChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


@dataclass(frozen=True, slots=True)
class ProjectInvoiceTriggerRunnerConfig:
    """Selects QuickBooks Trigger bindings of the project configuration."""

    invoice_changed_routes: Sequence[ProjectInvoiceChangedTriggerRoute]
    """The invoiceChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives the runner's and the durable inboxes' records; None uses standard logging."""


def new_project_invoice_trigger_runner(
    project: LoadedProject, connection: Connection, config: ProjectInvoiceTriggerRunnerConfig
) -> PollRunner:
    """Create a durable runner for invoiceChanged bindings of the loaded project.

    connection is the project connection from `new_project_connection(project, name)`; the caller owns it
    and closes it after the runner stops. Each route records changes in its binding's project Trigger inbox
    before its watermark commits in the binding's project poll cursor, so a restarted or replaced replica
    neither misses nor repeats a change. A binding without saved configuration is valid. Raises ValueError
    as `new_invoice_trigger_runner` does and for an invalid saved configuration.
    """
    if project is None:
        raise ValueError("QuickBooks invoiceChanged Triggers require a loaded project")
    _connection_client(connection)
    connection_name = connection.reference.name
    routes: list[InvoiceChangedTriggerRoute] = []
    for route in config.invoice_changed_routes:
        if route is None or route.target is None:
            raise ValueError("QuickBooks invoiceChanged binding target is required")
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_invoice_changed_trigger_configuration
            )
        except ObjectNotFoundError:
            configuration = InvoiceChangedTriggerConfiguration()
        except ValueError as error:
            raise ValueError(
                f"quickbooks invoiceChanged binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_invoice_changed_event_payload,
            logger=config.logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        routes.append(InvoiceChangedTriggerRoute(route.binding_name, target, configuration, cursor))
    return new_invoice_trigger_runner(InvoiceTriggerRunnerConfig(connection, routes, config.logger))


def new_invoice_changed_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    target: TriggerTarget[InvoiceChangedEvent],
    configuration: InvoiceChangedTriggerConfiguration | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one invoiceChanged Trigger with an in-memory cursor, as Go's `NewInvoiceChangedTrigger`.

    It has no project storage, so it records a new baseline on every start and changes made while it is
    stopped are lost; use `new_project_invoice_trigger_runner` for durable delivery. Raises ValueError when
    connection_name differs from the connection's name or for a mock connection.
    """
    client = _connection_client(connection)
    if connection_name != connection.reference.name:
        raise ValueError(
            f"quickbooks connector trigger connection_name {json.dumps(connection_name)} must equal its "
            f"connection's name {json.dumps(connection.reference.name)}"
        )
    if configuration is not None and not isinstance(configuration, InvoiceChangedTriggerConfiguration):
        raise ValueError("QuickBooks invoiceChanged configuration is invalid")
    binding = TriggerBindingRef(
        connection.reference, INVOICE_CHANGED_TRIGGER_DEFINITION.trigger, binding_name
    )
    source = new_binding_trigger_source(
        BindingPollerConfig(
            definition=INVOICE_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            cursor=new_memory_cursor_store(),
            fetcher=InvoiceChangeFetcher(client, connection.reference),
        ),
        _poll_schedule(client),
    )
    return new_trigger(
        TriggerConfig(
            definition=INVOICE_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            source=source,
            target=target,
            logger=logger,
        )
    )


__all__ = [
    "INVOICE_CHANGED_TRIGGER_DEFINITION",
    "INVOICE_CHANGE_CREATED",
    "INVOICE_CHANGE_UPDATED",
    "InvoiceChangeFetcher",
    "InvoiceChangedTriggerRoute",
    "InvoiceTriggerRunnerConfig",
    "ProjectInvoiceChangedTriggerRoute",
    "ProjectInvoiceTriggerRunnerConfig",
    "decode_invoice_changed_event_payload",
    "decode_invoice_changed_trigger_configuration",
    "define_invoice_changed_trigger_binding",
    "new_invoice_changed_trigger",
    "new_invoice_trigger_runner",
    "new_project_invoice_trigger_runner",
]
