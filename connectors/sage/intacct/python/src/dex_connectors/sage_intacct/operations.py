# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Sage Intacct invoice Query decisions matching Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any, TypeVar
from urllib.parse import quote

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import GetInvoiceInput, InvoiceList, InvoiceSummary, ListInvoicesInput

_INVOICE_KEY_PATTERN = re.compile(r"[A-Za-z0-9_-]{1,128}\Z")


_Output = TypeVar("_Output")
_INVOICES_PATH = "/ia/api/v1/objects/accounts-receivable/invoice"


def _branch(
    operation: str, empty: _Output, branch: str, kind: FailureKind, message: str
) -> QueryAttempt[_Output]:
    failure = Failure(kind, "sage-intacct", operation, message)
    return QueryAttempt.branch_result(branch, empty, Receipt(), failure)


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(
            body, object_pairs_hook=lambda pairs: {key.lower(): item for key, item in pairs}
        )
    except (ValueError, UnicodeDecodeError, RecursionError):
        return None
    return value if isinstance(value, dict) else None


def _summary(value: object) -> InvoiceSummary | None:
    if not isinstance(value, dict):
        return None
    key, invoice_id = value.get("key"), value.get("id")
    if not isinstance(key, str) or not _INVOICE_KEY_PATTERN.fullmatch(key):
        return None
    return InvoiceSummary(key, invoice_id) if isinstance(invoice_id, str) and invoice_id else None


def _read_failure(exchange: Exchange, operation: str, empty: _Output) -> QueryAttempt[_Output] | None:
    def retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[_Output]:
        return QueryAttempt.retry(Failure(kind, "sage-intacct", operation, message), delay)

    if exchange.error == "credential":
        return _branch(
            operation,
            empty,
            "defect",
            FailureKind.AUTHENTICATION,
            "Sage Intacct connection needs authorization",
        )
    if exchange.error in ("request", "refresh"):
        return retry(FailureKind.TRANSPORT, "Sage Intacct invoice request failed")
    if exchange.status == 429:
        return retry(FailureKind.RATE_LIMIT, "Sage Intacct rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return retry(
            FailureKind.AVAILABILITY, "Sage Intacct is temporarily unavailable", exchange.retry_after
        )
    if exchange.status == 404 and operation == "getInvoice":
        return _branch(
            operation, empty, "notFound", FailureKind.NOT_FOUND, "Sage Intacct invoice was not found"
        )
    if exchange.status != 200:
        return _branch(
            operation,
            empty,
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Sage Intacct rejected HTTP {exchange.status}",
        )
    if exchange.error == "too_large":
        return _branch(
            operation,
            empty,
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Sage Intacct invoice response exceeded the configured limit",
        )
    if exchange.error:
        return retry(FailureKind.TRANSPORT, "Sage Intacct invoice response was interrupted")
    return None


class GetInvoiceOperation:
    """Read one accounts receivable invoice by system-assigned key."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated invoice Query contract."""
        return definitions.GET_INVOICE_DEFINITION

    async def invoke(self, call: Call, input: GetInvoiceInput) -> QueryAttempt[InvoiceSummary]:
        """Project only the stable key and displayed ID into the Flow."""
        if not _INVOICE_KEY_PATTERN.fullmatch(input.invoice_key):
            return _branch(
                "getInvoice", InvoiceSummary(), "defect", FailureKind.VALIDATION, "invoice key is invalid"
            )
        exchange = await self.client.get(call, _INVOICES_PATH + "/" + quote(input.invoice_key, safe=""))
        failure = _read_failure(exchange, "getInvoice", InvoiceSummary())
        if failure is not None:
            return failure
        document = _document(exchange.body)
        invoice = _summary(document.get("ia::result")) if document else None
        if invoice is None or invoice.key != input.invoice_key:
            return _branch(
                "getInvoice",
                InvoiceSummary(),
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Sage Intacct returned invalid invoice JSON",
            )
        return QueryAttempt.branch_result("found", invoice, Receipt())


class ListInvoicesOperation:
    """Read the first page of Accounts Receivable invoice keys and IDs."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated invoice list contract."""
        return definitions.LIST_INVOICES_DEFINITION

    async def invoke(self, call: Call, input: ListInvoicesInput) -> QueryAttempt[InvoiceList]:
        """Return the next link as opaque provider data after one GET."""
        exchange = await self.client.get(call, _INVOICES_PATH)
        failure = _read_failure(exchange, "listInvoices", InvoiceList())
        if failure is not None:
            return failure
        document = _document(exchange.body)
        rows = document.get("ia::result") if document else None
        meta = document.get("ia::meta") if document else None
        next_link = meta.get("next") if isinstance(meta, dict) else None
        if (
            not isinstance(rows, list)
            or (meta is not None and not isinstance(meta, dict))
            or (next_link is not None and not isinstance(next_link, str))
        ):
            return _branch(
                "listInvoices",
                InvoiceList(),
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Sage Intacct returned invalid invoice list JSON",
            )
        invoices = []
        for row in rows:
            invoice = _summary(row)
            if invoice is None:
                return _branch(
                    "listInvoices",
                    InvoiceList(),
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Sage Intacct returned an incomplete invoice identity",
                )
            invoices.append(invoice)
        return QueryAttempt.branch_result("found", InvoiceList(tuple(invoices), next_link or ""), Receipt())
