# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the invoice change example's Trigger mapping and Flow wiring."""

from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerEvent

from dex_connectors.intuit_quickbooks import (
    INVOICE_CHANGE_UPDATED,
    Client,
    Config,
    Credentials,
    Invoice,
    InvoiceChangedEvent,
    new_connection,
)
from examples.invoice_changed.flow import (
    CONNECTION_NAME,
    INVOICE_CHANGED_TRIGGER_BINDING,
    InvoiceChangedFlow,
    accepts_every_invoice_change,
    map_to_invoice_change,
    resolve_invoice_change_flow_id,
)

EVENT = TriggerEvent(
    "qbo-invoice-ae56cc0fa7319340be6402e6ffb5b852",
    datetime(2026, 11, 2, 16, 0, 2, tzinfo=UTC),
    InvoiceChangedEvent(
        realm_id="9341453050298464",
        environment="production",
        change_kind=INVOICE_CHANGE_UPDATED,
        invoice=Invoice(
            invoice_id="132", sync_token="1", customer_id="58", total_amount="25.50", balance="0"
        ),
    ),
)


def new_flow(connection_name: str) -> InvoiceChangedFlow:
    """Build the example Flow over a connection named connection_name that never reaches a provider."""
    reference = ConnectionRef("quickbooks", connection_name)
    credentials = Credentials("c", SecretString("s"), SecretString("a"), SecretString("r"), SecretString(""))
    client = Client(Config(realm_id="9341453050298464"), StaticCredentialProvider({reference: credentials}))
    return InvoiceChangedFlow(new_connection(client, reference))


def test_each_invoice_version_names_one_flow_and_keeps_its_event_id() -> None:
    """A redelivered version finds its Flow; the input carries the event ID beside the change."""
    assert resolve_invoice_change_flow_id(EVENT) == (
        "quickbooks-invoice-change-qbo-invoice-ae56cc0fa7319340be6402e6ffb5b852"
    )
    change = map_to_invoice_change(EVENT)
    assert change.trigger_event_id == EVENT.id
    assert change.change == EVENT.payload
    assert accepts_every_invoice_change(EVENT)


def test_flow_declares_the_static_binding_and_requires_its_connection() -> None:
    """The analyzer reads one static binding, and another connection name is refused."""
    flow = new_flow(CONNECTION_NAME)
    assert flow.get_flow_type() == "QuickBooksInvoiceChangePython"
    [binding] = flow.get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (
        CONNECTION_NAME,
        INVOICE_CHANGED_TRIGGER_BINDING,
    )
    with pytest.raises(ValueError):
        new_flow("other")
