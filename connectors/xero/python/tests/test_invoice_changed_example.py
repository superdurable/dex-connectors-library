# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the invoice change example's Trigger mapping and Flow wiring."""

from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerEvent

from dex_connectors.xero import Client, Config, Credentials, Invoice, InvoiceChangedEvent, new_connection
from examples.invoice_changed.flow import (
    CONNECTION_NAME,
    INVOICE_CHANGED_TRIGGER_BINDING,
    InvoiceChangedFlow,
    accepts_every_invoice_change,
    map_to_invoice_change,
    resolve_invoice_change_flow_id,
)

EVENT = TriggerEvent(
    "xero-invoice-2320ceb3271e677af3a09fb91ca96e10",
    datetime(2026, 10, 1, 9, 0, 2, 500_000, tzinfo=UTC),
    InvoiceChangedEvent(
        invoice=Invoice(
            invoice_id="8a1d0e2f-0000-4000-8000-00000000000b", type="ACCREC", status="DRAFT", total="20.00"
        )
    ),
)


def new_flow(connection_name: str) -> InvoiceChangedFlow:
    """Build the example Flow over a connection named connection_name that never reaches a provider."""
    reference = ConnectionRef("xero", connection_name)
    credentials = Credentials(auth_method_id="custom-connection", access_token=SecretString("inert"))
    client = Client(Config(), StaticCredentialProvider({reference: credentials}))
    return InvoiceChangedFlow(new_connection(client, reference))


def test_each_change_names_one_flow_and_keeps_its_event_id() -> None:
    """A redelivered change finds its Flow; the input carries the event ID beside the change."""
    assert resolve_invoice_change_flow_id(EVENT) == (
        "xero-invoice-change-xero-invoice-2320ceb3271e677af3a09fb91ca96e10"
    )
    change = map_to_invoice_change(EVENT)
    assert change.trigger_event_id == EVENT.id
    assert change.change == EVENT.payload
    assert accepts_every_invoice_change(EVENT)


def test_flow_declares_the_static_binding_with_its_filter_unit_and_requires_its_connection() -> None:
    """The analyzer reads one static binding with the invoice filter, and another connection is refused."""
    flow = new_flow(CONNECTION_NAME)
    assert flow.get_flow_type() == "XeroInvoiceChangePython"
    [binding] = flow.get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (
        CONNECTION_NAME,
        INVOICE_CHANGED_TRIGGER_BINDING,
    )
    assert binding.configuration_ui is not None
    [unit] = binding.configuration_ui.units
    assert unit.unit_id == "invoiceChangeFilter"
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [
        ("type", "/type"),
        ("statuses", "/statuses"),
    ]
    with pytest.raises(ValueError):
        new_flow("other")
