# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Sage Intacct invoice Query decisions matching Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any
from urllib.parse import quote

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import GetInvoiceInput, InvoiceSummary

_INVOICE_KEY_PATTERN = re.compile(r"[A-Za-z0-9_-]{1,128}\Z")


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "sage-intacct", "getInvoice", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[InvoiceSummary]:
    return QueryAttempt.branch_result(branch, InvoiceSummary(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[InvoiceSummary]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _read_failure(exchange: Exchange) -> QueryAttempt[InvoiceSummary] | None:
    if exchange.error == "credential":
        return _branch("defect", FailureKind.AUTHENTICATION, "Sage Intacct connection needs authorization")
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "Sage Intacct invoice request failed")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "Sage Intacct rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(
            FailureKind.AVAILABILITY, "Sage Intacct is temporarily unavailable", exchange.retry_after
        )
    if exchange.status == 404:
        return _branch("notFound", FailureKind.NOT_FOUND, "Sage Intacct invoice was not found")
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
            "Sage Intacct invoice response exceeded the configured limit",
        )
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION,
            f"Sage Intacct rejected HTTP {exchange.status}",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "Sage Intacct invoice response was interrupted")
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
            return _branch("defect", FailureKind.VALIDATION, "invoice key is invalid")
        exchange = await self.client.get(
            call, "/ia/api/v1/objects/accounts-receivable/invoice/" + quote(input.invoice_key, safe="")
        )
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        invoice = document.get("ia::result") if document else None
        if not isinstance(invoice, dict):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Sage Intacct returned invalid invoice JSON"
            )
        key, invoice_id = invoice.get("key"), invoice.get("id")
        if key != input.invoice_key or not isinstance(invoice_id, str) or not invoice_id:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Sage Intacct returned invalid invoice JSON"
            )
        return QueryAttempt.branch_result("found", InvoiceSummary(key, invoice_id), Receipt())
