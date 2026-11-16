# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the invoiceChanged runners, binding factory, configuration decoding, and in-memory Trigger."""

import asyncio
from collections.abc import Callable
from datetime import UTC, datetime

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

from dex_connectors.xero import (
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
from dex_connectors.xero.invoice_change_fetcher import (
    invoice_change_event_id,
    invoice_change_filter_key,
    validate_invoice_changed_trigger_configuration,
)
from examples.invoice_changed.invoice_fake import XeroInvoiceFake

REFERENCE = ConnectionRef("xero", "xero-invoice-watcher")
INVOICE_ID = "8a1d0e2f-0000-4000-8000-00000000000b"


class RecordingTarget:
    """Remember every delivered event."""

    def __init__(self) -> None:
        self.events: list[TriggerEvent[InvoiceChangedEvent]] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[InvoiceChangedEvent]) -> None:
        self.events.append(event)


def new_client(fake: XeroInvoiceFake, poll_interval_seconds: float = 30) -> Client:
    """Build a Custom Connection client against the in-process fake."""
    credentials = Credentials(auth_method_id="custom-connection", access_token=SecretString("inert-access"))
    return Client(
        Config(poll_interval_seconds=poll_interval_seconds),
        StaticCredentialProvider({REFERENCE: credentials}),
        httpx.AsyncClient(transport=fake.transport()),
        local_provider_url="http://127.0.0.1:1",
    )


def test_client_rejects_a_poll_interval_under_thirty_seconds() -> None:
    """Every poll spends the organisation's daily limit, so the Go minimum applies."""
    with pytest.raises(ValueError, match="at least 30s"):
        new_client(XeroInvoiceFake(), 29.9)


def test_client_keeps_the_default_poll_interval_for_zero() -> None:
    """A zero pollInterval, as a saved project may hold, keeps the five-minute default, as Go does."""
    assert new_client(XeroInvoiceFake(), 0).config.poll_interval_seconds == 300


def test_binding_factory_and_strict_configuration_decoding() -> None:
    """The generated factory names the Trigger, and decoding is strict as Go's is."""
    binding = define_invoice_changed_trigger_binding(
        connection_name="xero-invoice-watcher", binding_name="invoice-changes"
    )
    assert binding.definition is INVOICE_CHANGED_TRIGGER_DEFINITION
    assert INVOICE_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name == "invoiceChanged"
    assert decode_invoice_changed_trigger_configuration({}) == InvoiceChangedTriggerConfiguration()
    decoded = decode_invoice_changed_trigger_configuration({"type": "ACCPAY", "statuses": ["PAID"]})
    assert decoded == InvoiceChangedTriggerConfiguration(type="ACCPAY", statuses=("PAID",))
    with pytest.raises(ValueError, match="unknown field"):
        decode_invoice_changed_trigger_configuration({"contactId": "x"})
    with pytest.raises(ValueError, match="must be a list"):
        decode_invoice_changed_trigger_configuration({"statuses": "PAID"})


def test_configuration_validation_matches_go() -> None:
    """Blank filters are valid; an unknown type or status and a repeated status are rejected."""
    validate_invoice_changed_trigger_configuration(InvoiceChangedTriggerConfiguration())
    validate_invoice_changed_trigger_configuration(
        InvoiceChangedTriggerConfiguration(type="ACCPAY", statuses=("PAID",))
    )
    with pytest.raises(ValueError, match="type must be ACCREC"):
        validate_invoice_changed_trigger_configuration(InvoiceChangedTriggerConfiguration(type="SALES"))
    with pytest.raises(ValueError, match="statuses must contain"):
        validate_invoice_changed_trigger_configuration(InvoiceChangedTriggerConfiguration(statuses=("OPEN",)))
    with pytest.raises(ValueError, match="more than once"):
        validate_invoice_changed_trigger_configuration(
            InvoiceChangedTriggerConfiguration(statuses=("PAID", "PAID"))
        )
    sales = InvoiceChangedTriggerConfiguration(type="ACCREC", statuses=("PAID", "AUTHORISED"))
    assert invoice_change_filter_key(sales) == "type=ACCREC;statuses=AUTHORISED,PAID"


def test_runner_validates_routes_and_filters() -> None:
    """A runner needs routes, binding names, targets, a provider connection, and a valid filter."""
    connection = new_connection(new_client(XeroInvoiceFake()), REFERENCE)
    with pytest.raises(ValueError, match="routes are required"):
        new_invoice_trigger_runner(InvoiceTriggerRunnerConfig(connection, []))
    with pytest.raises(ValueError, match="binding name and target"):
        route = InvoiceChangedTriggerRoute(" ", RecordingTarget())
        new_invoice_trigger_runner(InvoiceTriggerRunnerConfig(connection, [route]))
    with pytest.raises(ValueError, match="xero invoiceChanged configuration: type must be"):
        route = InvoiceChangedTriggerRoute(
            "b", RecordingTarget(), InvoiceChangedTriggerConfiguration(type="X")
        )
        new_invoice_trigger_runner(InvoiceTriggerRunnerConfig(connection, [route]))
    with pytest.raises(ValueError, match="must equal"):
        new_invoice_changed_trigger(
            connection=connection, connection_name="other", binding_name="b", target=RecordingTarget()
        )


def test_event_id_is_a_pure_function_of_the_invoice_and_its_change_instant() -> None:
    """The ID hashes the lower-case invoice ID and UpdatedDateUTC milliseconds, as Go does."""
    updated = datetime(2026, 10, 1, 9, 0, 2, 500_000, tzinfo=UTC)
    identifier = invoice_change_event_id(INVOICE_ID, updated)
    assert identifier == "xero-invoice-2320ceb3271e677af3a09fb91ca96e10"
    assert invoice_change_event_id(INVOICE_ID.upper(), updated) == identifier
    assert invoice_change_event_id(INVOICE_ID, updated.replace(microsecond=501_000)) != identifier


async def wait_until(condition: Callable[[], bool], timeout: float = 10) -> None:
    """Poll condition until it holds; convergence polling, never a guessed sleep."""
    deadline = asyncio.get_running_loop().time() + timeout
    while not condition():
        assert asyncio.get_running_loop().time() < deadline, "condition did not converge"
        await asyncio.sleep(0.01)


async def test_in_memory_trigger_records_a_baseline_with_the_binding_filter() -> None:
    """The in-memory Trigger seeds from the most recently changed invoices of its filter on start."""
    fake = XeroInvoiceFake()
    client = new_client(fake)
    trigger = new_invoice_changed_trigger(
        connection=new_connection(client, REFERENCE),
        connection_name=REFERENCE.name,
        binding_name="invoice-changes",
        target=RecordingTarget(),
        configuration=InvoiceChangedTriggerConfiguration(type="ACCREC"),
    )
    context = TriggerContext()
    task = asyncio.create_task(trigger.run(context))
    try:
        await wait_until(lambda: bool(fake.list_requests()))
        [seed] = fake.list_requests()
        assert seed.query["order"] == ["UpdatedDateUTC DESC"] and seed.query["where"] == ['Type=="ACCREC"']
        assert seed.modified_since == ""
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await client.aclose()
        await client._http_client.aclose()


async def test_runner_with_an_in_memory_cursor_delivers_each_matching_change_once() -> None:
    """A route without a cursor store seeds, then a woken poll delivers only matching new changes."""
    fake = XeroInvoiceFake()
    fake.upsert("8a1d0e2f-0000-4000-8000-00000000000a", "INV-OLD", "ACCREC", "PAID", "1.00")
    fake.advance_clock(60_000)
    client = new_client(fake)
    target = RecordingTarget()
    route = InvoiceChangedTriggerRoute(
        "invoice-changes", target, InvoiceChangedTriggerConfiguration(type="ACCREC")
    )
    runner = new_invoice_trigger_runner(
        InvoiceTriggerRunnerConfig(new_connection(client, REFERENCE), [route])
    )
    context = TriggerContext()
    task = asyncio.create_task(runner.run(context))
    try:
        await wait_until(lambda: bool(fake.list_requests()))
        fake.upsert(INVOICE_ID, "INV-NEW", "ACCREC", "DRAFT", "2.00")
        fake.upsert("8a1d0e2f-0000-4000-8000-00000000000c", "BILL-1", "ACCPAY", "DRAFT", "3.00")
        runner.wake_poller("invoice-changes")
        await wait_until(lambda: bool(target.events))
        assert [event.payload.invoice.invoice_number for event in target.events] == ["INV-NEW"]
        assert target.events[0].payload.tenant_id == ""
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await client.aclose()
        await client._http_client.aclose()
