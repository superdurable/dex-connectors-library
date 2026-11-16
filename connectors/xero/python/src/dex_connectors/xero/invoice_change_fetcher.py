# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll GET /Invoices ordered by UpdatedDateUTC for the invoiceChanged Trigger, at parity with Go."""

import hashlib
import json
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from decimal import Decimal as ExactDecimal
from typing import Final

from dex_connectors.sdk import ConnectionRef, FailureKind, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)

from ._generated.definitions import INVOICE_CHANGED_TRIGGER_DEFINITION
from .client import Client, Exchange
from .invoice_change_cursor import (
    InvoiceChangeCursor,
    InvoiceChangeCursorInvalidError,
    decode_invoice_change_cursor,
    encode_invoice_change_cursor,
    invoice_change_window_start,
    prune_seen_invoice_changes,
)
from .models import Invoice, InvoiceChangedEvent, InvoiceChangedTriggerConfiguration
from .operations import _dotnet, _exact_integer, _field, _invoice, _reject_nonfinite

INVOICE_CHANGE_PAGE_SIZE: Final = 100
"""Xero's documented paged-invoice size; every paged invoice carries its line items."""
MAXIMUM_INVOICE_CHANGE_WINDOW_PAGES: Final = 1_000
"""The most pages of invoices changed within one overlap window that a binding pages through."""
INVOICE_STATUSES: Final = ("DRAFT", "SUBMITTED", "DELETED", "AUTHORISED", "PAID", "VOIDED")
"""Every Xero invoice status a configuration may filter on."""
INVOICE_TYPES: Final = ("ACCREC", "ACCPAY")
"""ACCREC sales invoices and ACCPAY bills."""

_OPERATION: Final = "invoiceChanged"
_ASCENDING_UPDATE_ORDER: Final = "UpdatedDateUTC ASC"
_DESCENDING_UPDATE_ORDER: Final = "UpdatedDateUTC DESC"
_ZERO_TIME: Final = datetime.min.replace(tzinfo=UTC)
_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)


def validate_invoice_changed_trigger_configuration(configuration: InvoiceChangedTriggerConfiguration) -> None:
    """Reject an unknown type or status and a repeated status, as Go's Validate does; blank is valid."""
    if configuration is None or not isinstance(configuration, InvoiceChangedTriggerConfiguration):
        raise ValueError("Xero invoiceChanged configuration is invalid")
    if configuration.type and configuration.type not in INVOICE_TYPES:
        raise ValueError("type must be ACCREC for a sales invoice or ACCPAY for a bill")
    statuses = tuple(configuration.statuses)
    if any(status not in INVOICE_STATUSES for status in statuses):
        raise ValueError(
            "statuses must contain Xero statuses: DRAFT, SUBMITTED, DELETED, AUTHORISED, PAID, or VOIDED"
        )
    for index, status in enumerate(statuses):
        if status in statuses[:index]:
            raise ValueError(f"statuses lists {status} more than once")


def invoice_change_filter_key(configuration: InvoiceChangedTriggerConfiguration) -> str:
    """Return the canonical filter the cursor records, so a changed filter records a new baseline."""
    statuses = ",".join(sorted(configuration.statuses))
    return "type=" + (configuration.type or "") + ";statuses=" + statuses


def unix_milliseconds(moment: datetime) -> int:
    """Return an aware datetime as whole milliseconds since the Unix epoch, as Go's UnixMilli does."""
    return (moment - _EPOCH) // timedelta(milliseconds=1)


def invoice_change_event_id(invoice_id: str, updated_at: datetime) -> str:
    """Hash the invoice ID and change instant, so a refetch yields the same short, URL-safe ID."""
    text = invoice_id.lower() + "\x00" + str(unix_milliseconds(updated_at))
    return "xero-invoice-" + hashlib.sha256(text.encode()).hexdigest()[:32]


def seen_invoice_change_key(invoice: Invoice) -> str:
    """Name a delivered change sortably: 13-digit milliseconds, then a hash of the invoice ID."""
    digest = hashlib.sha256(invoice.invoice_id.lower().encode()).hexdigest()[:12]
    return f"{unix_milliseconds(invoice.updated_at):013d}-{digest}"


def _new_invoice_changed_trigger_event(invoice: Invoice, tenant_id: str) -> TriggerEvent[InvoiceChangedEvent]:
    return TriggerEvent(
        invoice_change_event_id(invoice.invoice_id, invoice.updated_at),
        invoice.updated_at.astimezone(UTC),
        InvoiceChangedEvent(invoice=invoice, tenant_id=tenant_id),
    )


@dataclass(slots=True)
class _ListedInvoiceChanges:
    """One decoded page with Xero's server time and the tenant the request used."""

    invoices: list[Invoice] = field(default_factory=list)
    server_time: datetime | None = None
    tenant_id: str = ""
    has_more_pages: bool = False


def _invoice_change_poll_failure(exchange: Exchange) -> Exception:
    """Pause the connection for Xero's minute, concurrency, and daily limits; otherwise back off."""
    message = exchange.error.message if exchange.error is not None else "Xero returned no response"
    failure = RuntimeError("Xero invoiceChanged poll: " + message)
    if exchange.outcome == "daily":
        # The daily limit resets hours later; Retry-After carries the full delay, uncapped.
        seconds = (exchange.metadata or {}).get("retryAfterSeconds", "")
        return mark_rate_limited(failure, timedelta(seconds=int(seconds) if seconds.isdigit() else 0))
    if (
        exchange.outcome == "retry"
        and exchange.error is not None
        and exchange.error.kind == FailureKind.RATE_LIMIT
    ):
        return mark_rate_limited(failure, timedelta(seconds=exchange.retry_after))
    return failure


class InvoiceChangeFetcher:
    """Poll GET /Invoices ordered by UpdatedDateUTC for the poll Trigger kit, at parity with Go.

    It implements `ChangeFetcher[TriggerEvent[InvoiceChangedEvent]]`. The cursor is the newest delivered
    UpdatedDateUTC and the changes already delivered inside the five-second re-read window. A poller owns
    one fetcher and calls it from one task at a time.
    """

    def __init__(
        self,
        client: Client,
        connection: ConnectionRef,
        configuration: InvoiceChangedTriggerConfiguration | None = None,
    ) -> None:
        """Bind the fetcher to one connection's client and one validated binding configuration."""
        if client is None or connection is None:
            raise ValueError("Xero invoiceChanged client and connection are required")
        self._configuration = configuration or InvoiceChangedTriggerConfiguration()
        validate_invoice_changed_trigger_configuration(self._configuration)
        self._client = client
        self._call = new_trigger_credential_call(connection, INVOICE_CHANGED_TRIGGER_DEFINITION.trigger)

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Read the most recently changed invoices once and start at Xero's server time.

        Existing invoices become the baseline and are never delivered.
        """
        listed = await self._list_page(context, None, 1, _DESCENDING_UPDATE_ORDER)
        since = listed.server_time or self._client._now().astimezone(UTC)
        for invoice in listed.invoices:
            if invoice.updated_at > since:
                since = invoice.updated_at
        position = InvoiceChangeCursor(
            invoice_change_filter_key(self._configuration), listed.tenant_id, unix_milliseconds(since)
        )
        window_start = invoice_change_window_start(position.since)
        for invoice in listed.invoices:
            if unix_milliseconds(invoice.updated_at) >= window_start:
                position.seen.append(seen_invoice_change_key(invoice))
        position.seen = prune_seen_invoice_changes(position.seen, position.since)
        return encode_invoice_change_cursor(position)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[InvoiceChangedEvent]]:
        """Read one page of invoices changed since the cursor's overlap window, skipping delivered changes.

        A cursor that cannot be decoded, or one for another filter or organisation, raises
        `mark_cursor_invalid`, so the kit records a new baseline; a Xero 429 raises `mark_rate_limited`.
        """
        try:
            position = decode_invoice_change_cursor(cursor.token)
        except InvoiceChangeCursorInvalidError as error:
            raise mark_cursor_invalid(error) from None
        if position.since <= 0 or position.page < 0:
            raise mark_cursor_invalid(InvoiceChangeCursorInvalidError("Xero invoice cursor is not decodable"))
        if position.filter != invoice_change_filter_key(self._configuration):
            raise mark_cursor_invalid(ValueError("Xero invoice cursor belongs to another invoice filter"))
        page_number = max(position.page, 1)
        window_start = invoice_change_window_start(position.since)
        listed = await self._list_page(context, window_start, page_number, _ASCENDING_UPDATE_ORDER)
        if listed.tenant_id != position.tenant_id:
            raise mark_cursor_invalid(ValueError("Xero invoice cursor belongs to another organisation"))
        seen = set(position.seen)
        following = InvoiceChangeCursor(
            position.filter, position.tenant_id, position.since, position.page, list(position.seen)
        )
        newest_update = position.since
        changes: list[TriggerEvent[InvoiceChangedEvent]] = []
        for invoice in listed.invoices:
            if invoice.updated_at == _ZERO_TIME:
                raise ValueError("Xero listed an invoice without UpdatedDateUTC")
            updated = unix_milliseconds(invoice.updated_at)
            newest_update = max(newest_update, updated)
            key = seen_invoice_change_key(invoice)
            if updated < window_start or key in seen:
                continue
            seen.add(key)
            following.seen.append(key)
            changes.append(_new_invoice_changed_trigger_event(invoice, listed.tenant_id))
        has_more_pages = False
        if not listed.has_more_pages:
            following.since, following.page = newest_update, 0
        elif invoice_change_window_start(newest_update) > window_start:
            following.since, following.page, has_more_pages = newest_update, 0, True
        elif page_number >= MAXIMUM_INVOICE_CHANGE_WINDOW_PAGES:
            raise RuntimeError(
                f"more than {MAXIMUM_INVOICE_CHANGE_WINDOW_PAGES} pages of Xero invoices changed within "
                "one 5s window"
            )
        else:
            # Every invoice on this full page changed inside the window, so only the page number can advance.
            following.page, has_more_pages = page_number + 1, True
        following.seen = prune_seen_invoice_changes(following.seen, following.since)
        return ChangePage(tuple(changes), encode_invoice_change_cursor(following), has_more_pages)

    async def _list_page(
        self, context: TriggerContext, modified_since: int | None, page_number: int, order: str
    ) -> _ListedInvoiceChanges:
        """Send one GET /Invoices request with the binding's filters, line items, and the given order."""
        await wait_for_poll_request(context)
        query = {"page": str(page_number), "pageSize": str(INVOICE_CHANGE_PAGE_SIZE), "unitdp": "4"}
        if self._configuration.statuses:
            query["Statuses"] = ",".join(self._configuration.statuses)
        if self._configuration.type:
            query["where"] = f'Type=="{self._configuration.type}"'
        query["order"] = order
        since = None
        if modified_since is not None:
            since = _EPOCH + timedelta(milliseconds=modified_since)
        exchange = await self._client.exchange(
            self._call, _OPERATION, "GET", "/Invoices", query, modified_since=since
        )
        if exchange.outcome != "success":
            raise _invoice_change_poll_failure(exchange)
        return _decode_invoice_change_page(exchange, page_number)


def _decode_invoice_change_page(exchange: Exchange, page_number: int) -> _ListedInvoiceChanges:
    try:
        document = json.loads(
            exchange.body,
            parse_int=_exact_integer,
            parse_float=ExactDecimal,
            parse_constant=_reject_nonfinite,
        )
        elements = _field(document, "Invoices", list, None)
        server_time = _field(document, "DateTimeUTC", str, "")
        pagination = _field(document, "pagination", dict, None)
        page_count = 0
        if pagination is not None:
            # Go decodes every pagination member into an int, so any of them with another type fails the page.
            for name in ("page", "pageSize", "itemCount"):
                _field(pagination, name, int, 0)
            page_count = _field(pagination, "pageCount", int, 0)
    except (ValueError, TypeError):
        raise ValueError("Xero invoice page is not a JSON object with an Invoices list") from None
    if elements is None:
        raise ValueError("Xero invoice page is not a JSON object with an Invoices list")
    if len(elements) > MAXIMUM_CHANGES_PER_PAGE:
        raise ValueError(
            f"Xero invoice page has {len(elements)} invoices, more than {MAXIMUM_CHANGES_PER_PAGE}"
        )
    listed = _ListedInvoiceChanges(tenant_id=(exchange.metadata or {}).get("tenantId", ""))
    if server_time:
        try:
            listed.server_time = _dotnet(server_time)
        except (ValueError, OverflowError):
            raise ValueError("Xero invoice page has an invalid DateTimeUTC") from None
    for element in elements:
        try:
            listed.invoices.append(_invoice(element))
        except (ValueError, TypeError, KeyError, OverflowError) as error:
            raise ValueError(f"Xero invoice page has an invalid invoice: {error}") from None
    if page_count > 0:
        listed.has_more_pages = page_number < page_count
    else:
        listed.has_more_pages = len(listed.invoices) >= INVOICE_CHANGE_PAGE_SIZE
    return listed
