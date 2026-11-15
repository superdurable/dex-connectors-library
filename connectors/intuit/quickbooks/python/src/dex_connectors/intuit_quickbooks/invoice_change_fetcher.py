# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll one company's invoices by MetaData.LastUpdatedTime for the invoiceChanged Trigger."""

import hashlib
from datetime import timedelta
from typing import Final

from dex_connectors.sdk import ConnectionRef, FailureKind, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)

from ._generated.definitions import INVOICE_CHANGED_TRIGGER_DEFINITION
from .client import Client
from .invoice_change_cursor import (
    INVOICE_CHANGE_LOOKBACK,
    MAXIMUM_SEEN_INVOICE_VERSIONS,
    SECOND,
    InvoiceChangeCursorInvalidError,
    InvoiceWindow,
    decode_invoice_change_cursor,
    encode_invoice_change_cursor,
    format_rfc3339_seconds,
    unix_nanoseconds,
)
from .models import Invoice, InvoiceChangedEvent
from .provider import decode_invoice, query_entities

INVOICE_CHANGE_CREATED: Final = "created"
"""An invoice at its first version: QuickBooks reports SyncToken 0."""
INVOICE_CHANGE_UPDATED: Final = "updated"
"""Any later version, including an edit, an applied payment, an email, and a void.

An invoice created and edited between two polls arrives once, as updated.
"""
INVOICE_CHANGE_PAGE_SIZE: Final = 100
"""The MAXRESULTS of each invoiceChanged query; QuickBooks itself accepts up to 1000."""

_OPERATION: Final = "invoiceChanged"
_MAXIMUM_SEED_PAGES: Final = MAXIMUM_SEEN_INVOICE_VERSIONS // INVOICE_CHANGE_PAGE_SIZE
_NEW_INVOICE_SYNC_TOKEN: Final = "0"


def invoice_version_key(invoice: Invoice) -> str:
    """Name one invoice version as "<Id>:<SyncToken>", as the cursor stores it."""
    return invoice.invoice_id + ":" + invoice.sync_token


def invoice_change_event_id(realm_id: str, environment: str, invoice: Invoice) -> str:
    """Return the Trigger event ID, a hash of the company, Id, SyncToken, and LastUpdatedTime.

    It is a pure function of the invoice version, so a refetch after a crash yields the same ID.
    """
    updated = format_rfc3339_seconds(unix_nanoseconds(invoice.updated_at)) if invoice.updated_at else ""
    text = "\x00".join([environment, realm_id, invoice.invoice_id, invoice.sync_token, updated])
    return "qbo-invoice-" + hashlib.sha256(text.encode()).hexdigest()[:32]


def _new_invoice_changed_trigger_event(
    realm_id: str, environment: str, invoice: Invoice
) -> TriggerEvent[InvoiceChangedEvent]:
    kind = INVOICE_CHANGE_CREATED if invoice.sync_token == _NEW_INVOICE_SYNC_TOKEN else INVOICE_CHANGE_UPDATED
    assert invoice.updated_at is not None  # The poll rejects an invoice without LastUpdatedTime.
    return TriggerEvent(
        invoice_change_event_id(realm_id, environment, invoice),
        invoice.updated_at,
        InvoiceChangedEvent(realm_id=realm_id, environment=environment, change_kind=kind, invoice=invoice),
    )


def _quote_query_literal(value: str) -> str:
    return "'" + value.replace("'", "\\'") + "'"


class InvoiceChangeFetcher:
    """Poll one company's invoices ordered by MetaData.LastUpdatedTime for the poll Trigger kit.

    It implements `ChangeFetcher[TriggerEvent[InvoiceChangedEvent]]` at parity with the Go connector: the
    cursor is a LastUpdatedTime window rereading 30 seconds before the newest change, and the
    "<Id>:<SyncToken>" versions delivered inside it. A poller owns one fetcher and calls it from one task
    at a time.
    """

    def __init__(self, client: Client, connection: ConnectionRef) -> None:
        """Bind the fetcher to one connection's client."""
        if client is None or connection is None:
            raise ValueError("QuickBooks invoiceChanged client and connection are required")
        self._client = client
        self._call = new_trigger_credential_call(connection, INVOICE_CHANGED_TRIGGER_DEFINITION.trigger)

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Record every version in the 30 seconds before the newest LastUpdatedTime as the baseline.

        Existing invoices are never delivered; an empty company delivers its first invoice.
        """
        realm_id = ""
        baseline: list[tuple[str, int]] = []
        newest: int | None = None
        is_window_complete = False
        for page in range(_MAXIMUM_SEED_PAGES):
            if is_window_complete:
                break
            statement = (
                "select * from Invoice ORDERBY MetaData.LastUpdatedTime DESC STARTPOSITION "
                f"{1 + page * INVOICE_CHANGE_PAGE_SIZE} MAXRESULTS {INVOICE_CHANGE_PAGE_SIZE}"
            )
            realm_id, invoices = await self._query_invoices(context, statement, False)
            is_window_complete = len(invoices) < INVOICE_CHANGE_PAGE_SIZE
            for invoice, second in invoices:
                if newest is None:
                    newest = second
                if second < newest - INVOICE_CHANGE_LOOKBACK:
                    is_window_complete = True
                    break
                baseline.append((invoice_version_key(invoice), second))
        window = InvoiceWindow()
        if baseline and newest is not None:
            window.since, window.from_ = newest, newest - INVOICE_CHANGE_LOOKBACK
            if not is_window_complete:
                # The scan stopped inside the window: its oldest second may be partial, so start after it.
                window.from_ = baseline[-1][1] + SECOND
            for key, second in baseline:
                if second >= window.from_:
                    window.seen[key] = second
            if window.from_ > window.since:
                window.from_ = window.since + SECOND
                window.since = window.from_
            window.prune_to_bound()
        return encode_invoice_change_cursor(realm_id, self._client.config.environment, window)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[InvoiceChangedEvent]]:
        """Read one page of invoices changed at or after the window's lower bound, oldest first.

        Versions already delivered or baselined are skipped. An undecodable cursor, one for another
        company or environment, or a QuickBooks validation Fault for the watermark query raises
        `mark_cursor_invalid`, so the kit records a new baseline; a 429 raises `mark_rate_limited`.
        """
        try:
            position, window = decode_invoice_change_cursor(cursor.token)
        except InvoiceChangeCursorInvalidError as error:
            raise mark_cursor_invalid(error) from None
        environment = self._client.config.environment
        if position.environment != environment:
            raise mark_cursor_invalid(ValueError("QuickBooks invoice cursor belongs to another environment"))
        statement = "select * from Invoice"
        if position.from_:
            statement += " where MetaData.LastUpdatedTime >= " + _quote_query_literal(position.from_)
        statement += (
            f" ORDERBY MetaData.LastUpdatedTime STARTPOSITION {1 + window.offset}"
            f" MAXRESULTS {INVOICE_CHANGE_PAGE_SIZE}"
        )
        realm_id, invoices = await self._query_invoices(context, statement, position.from_ != "")
        if realm_id != position.realm_id:
            raise mark_cursor_invalid(ValueError("QuickBooks invoice cursor belongs to another company"))
        changes: list[TriggerEvent[InvoiceChangedEvent]] = []
        previous = window.from_
        for invoice, second in invoices:
            if second < previous:
                raise ValueError("QuickBooks invoice page is not in LastUpdatedTime order")
            previous = second
            if invoice_version_key(invoice) in window.seen:
                continue
            changes.append(_new_invoice_changed_trigger_event(realm_id, environment, invoice))
        is_full_page = len(invoices) == INVOICE_CHANGE_PAGE_SIZE
        polled = [(invoice_version_key(invoice), second) for invoice, second in invoices]
        following = window.advance(polled, is_full_page)
        next_cursor = encode_invoice_change_cursor(position.realm_id, position.environment, following)
        return ChangePage(tuple(changes), next_cursor, is_full_page)

    async def _query_invoices(
        self, context: TriggerContext, statement: str, has_watermark: bool
    ) -> tuple[str, list[tuple[Invoice, int]]]:
        """Send one invoice query; return its company and its invoices with their LastUpdatedTime second."""
        await wait_for_poll_request(context)
        exchange = await self._client.exchange(self._call, _OPERATION, "GET", "/query", {"query": statement})
        if exchange.outcome != "succeeded":
            failure = exchange.failure
            message = failure.message if failure is not None else "QuickBooks returned no response"
            error = RuntimeError("QuickBooks invoice poll: " + message)
            if failure is not None and failure.kind == FailureKind.RATE_LIMIT:
                raise mark_rate_limited(error, timedelta(seconds=exchange.retry_after))
            if (
                has_watermark
                and exchange.status == 400
                and failure is not None
                and failure.kind == FailureKind.VALIDATION
            ):
                raise mark_cursor_invalid(error)
            raise error
        realm_id = exchange.metadata.get("realmId", "")
        try:
            elements = query_entities(exchange.body, "Invoice")
        except (ValueError, TypeError) as error:
            raise ValueError(f"QuickBooks invoice poll returned an invalid page: {error}") from None
        if len(elements) > INVOICE_CHANGE_PAGE_SIZE:
            raise ValueError("QuickBooks invoice poll returned more invoices than requested")
        invoices: list[tuple[Invoice, int]] = []
        for element in elements:
            try:
                invoice = decode_invoice(element)
            except (ValueError, TypeError, KeyError) as error:
                raise ValueError(f"QuickBooks invoice poll returned an invalid invoice: {error}") from None
            if invoice.updated_at is None:
                raise ValueError(
                    "QuickBooks invoice poll returned an invoice without MetaData.LastUpdatedTime"
                )
            nanoseconds = unix_nanoseconds(invoice.updated_at)
            invoices.append((invoice, nanoseconds - nanoseconds % SECOND))
        return realm_id, invoices
