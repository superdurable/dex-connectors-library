# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the invoiceChanged runners, binding factory, configuration decoding, and in-memory Trigger."""

import asyncio
from datetime import datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)

from dex_connectors.intuit_quickbooks import (
    INVOICE_CHANGE_CREATED,
    INVOICE_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    InvoiceChangedEvent,
    InvoiceChangedTriggerConfiguration,
    InvoiceChangedTriggerRoute,
    InvoiceTriggerRunnerConfig,
    decode_invoice_changed_trigger_configuration,
    define_invoice_changed_trigger_binding,
    new_connection,
    new_invoice_changed_trigger,
    new_invoice_trigger_runner,
)
from dex_connectors.intuit_quickbooks.invoice_change_fetcher import invoice_change_event_id
from dex_connectors.intuit_quickbooks.models import Invoice
from examples.invoice_changed.invoice_fake import QuickBooksInvoiceFake

REALM_ID = "9341453050298464"
REFERENCE = ConnectionRef("quickbooks", "quickbooks-invoice-watcher")


class RecordingTarget:
    """Remember every delivered event."""

    def __init__(self) -> None:
        self.events: list[TriggerEvent[InvoiceChangedEvent]] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[InvoiceChangedEvent]) -> None:
        self.events.append(event)


def new_client(fake: QuickBooksInvoiceFake, poll_interval_seconds: float = 5) -> Client:
    """Build a client against the in-process fake."""
    credentials = Credentials(
        "client", SecretString("secret"), SecretString("access"), SecretString("refresh"), SecretString("")
    )
    return Client(
        Config(realm_id=REALM_ID, poll_interval_seconds=poll_interval_seconds),
        StaticCredentialProvider({REFERENCE: credentials}),
        httpx.AsyncClient(transport=fake.transport()),
        local_provider_url="http://127.0.0.1:1",
    )


def test_client_rejects_a_poll_interval_under_five_seconds() -> None:
    """QuickBooks allows 500 requests per minute per company, so the Go minimum applies."""
    with pytest.raises(ValueError, match="at least 5s"):
        new_client(QuickBooksInvoiceFake(REALM_ID), 4.9)


def test_client_keeps_the_default_poll_interval_for_zero() -> None:
    """A zero pollInterval, as a saved project may hold, keeps the 30-second default, as Go does."""
    assert new_client(QuickBooksInvoiceFake(REALM_ID), 0).config.poll_interval_seconds == 30


def test_binding_factory_and_configuration_decoding() -> None:
    """The generated factory names the Trigger, and the empty configuration rejects unknown fields."""
    binding = define_invoice_changed_trigger_binding(
        connection_name="quickbooks-invoice-watcher", binding_name="quickbooks-invoice-changes"
    )
    assert binding.definition is INVOICE_CHANGED_TRIGGER_DEFINITION
    assert INVOICE_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name == "invoiceChanged"
    assert decode_invoice_changed_trigger_configuration({}) == InvoiceChangedTriggerConfiguration()
    with pytest.raises(ValueError, match="unknown field"):
        decode_invoice_changed_trigger_configuration({"customerId": "58"})


def test_runner_validates_routes() -> None:
    """A runner needs routes, binding names, targets, and a provider connection."""
    connection = new_connection(new_client(QuickBooksInvoiceFake(REALM_ID)), REFERENCE)
    with pytest.raises(ValueError, match="routes are required"):
        new_invoice_trigger_runner(InvoiceTriggerRunnerConfig(connection, []))
    with pytest.raises(ValueError, match="binding name and target"):
        route = InvoiceChangedTriggerRoute(" ", RecordingTarget())
        new_invoice_trigger_runner(InvoiceTriggerRunnerConfig(connection, [route]))
    with pytest.raises(ValueError, match="must equal"):
        new_invoice_changed_trigger(
            connection=connection, connection_name="other", binding_name="b", target=RecordingTarget()
        )


def test_event_id_is_a_pure_function_of_the_invoice_version() -> None:
    """The ID hashes environment, company, Id, SyncToken, and LastUpdatedTime second, as Go does."""
    updated = datetime.fromisoformat("2026-11-02T16:00:02.250+00:00")
    invoice = Invoice(invoice_id="132", sync_token="0", updated_at=updated)
    identifier = invoice_change_event_id(REALM_ID, "production", invoice)
    assert identifier == "qbo-invoice-ae56cc0fa7319340be6402e6ffb5b852"
    assert identifier != invoice_change_event_id(REALM_ID, "sandbox", invoice)


async def test_in_memory_trigger_seeds_then_delivers_each_new_version() -> None:
    """The in-memory Trigger records a baseline on start, then delivers a later invoice once."""
    fake = QuickBooksInvoiceFake(REALM_ID)
    fake.create("EXISTING", "58", "1.00")
    client = new_client(fake)
    target = RecordingTarget()
    trigger = new_invoice_changed_trigger(
        connection=new_connection(client, REFERENCE),
        connection_name=REFERENCE.name,
        binding_name="quickbooks-invoice-changes",
        target=target,
    )
    context = TriggerContext()
    task = asyncio.create_task(trigger.run(context))
    try:
        deadline = asyncio.get_running_loop().time() + 10
        while not fake.queries():
            assert asyncio.get_running_loop().time() < deadline
            await asyncio.sleep(0.01)
        created = fake.create("NEW", "58", "2.00")
        while not target.events:
            assert asyncio.get_running_loop().time() < deadline + timedelta(seconds=10).total_seconds()
            await asyncio.sleep(0.05)
        assert [event.payload.invoice.invoice_id for event in target.events] == [created]
        assert target.events[0].payload.change_kind == INVOICE_CHANGE_CREATED
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await client.http_client.aclose()
