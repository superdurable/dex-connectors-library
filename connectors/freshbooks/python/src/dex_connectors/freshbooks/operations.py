# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""FreshBooks getInvoice decisions matching the Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import GetInvoiceInput, Invoice

_ACCOUNT_ID = re.compile(r"^[A-Za-z0-9_-]{1,64}$")
_AMOUNT = re.compile(r"^-?[0-9]+(?:\.[0-9]+)?$")
_CURRENCY = re.compile(r"^[A-Z]{3}$")


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "freshbooks", "getInvoice", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[Invoice]:
    return QueryAttempt.branch_result(branch, Invoice(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Invoice]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _read_failure(exchange: Exchange) -> QueryAttempt[Invoice] | None:
    if exchange.error == "credential":
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "FreshBooks OAuth credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "FreshBooks request failed")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "FreshBooks rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(FailureKind.AVAILABILITY, "FreshBooks is temporarily unavailable", exchange.retry_after)
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "FreshBooks response exceeded the configured limit",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "FreshBooks response was interrupted")
    if exchange.status == 404:
        return QueryAttempt.branch_result("missing", Invoice(), Receipt())
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"FreshBooks rejected HTTP {exchange.status}"
        )
    return None


def _decode_invoice(body: bytes, input: GetInvoiceInput) -> Invoice | None:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    if not isinstance(document, dict):
        return None
    response = document.get("response")
    result = response.get("result") if isinstance(response, dict) else None
    invoice = result.get("invoice") if isinstance(result, dict) else None
    if not isinstance(invoice, dict):
        return None
    amount = invoice.get("amount")
    if not isinstance(amount, dict):
        return None
    account_id, invoice_id = invoice.get("accountid"), invoice.get("invoiceid")
    number, value, currency, status = (
        invoice.get("invoice_number"),
        amount.get("amount"),
        amount.get("code"),
        invoice.get("payment_status"),
    )
    if (
        account_id != input.account_id
        or invoice_id != input.invoice_id
        or isinstance(invoice_id, bool)
        or not isinstance(number, str)
        or not number
        or not isinstance(value, str)
        or not _AMOUNT.fullmatch(value)
        or not isinstance(currency, str)
        or not _CURRENCY.fullmatch(currency)
        or not isinstance(status, str)
        or not status
    ):
        return None
    return Invoice(account_id, invoice_id, number, value, currency, status)


class GetInvoiceOperation:
    """Read one invoice from the authorized accounting account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated invoice Query contract."""
        return definitions.GET_INVOICE_DEFINITION

    async def invoke(self, call: Call, input: GetInvoiceInput) -> QueryAttempt[Invoice]:
        """Read one bounded invoice response and project selected financial fields."""
        if (
            not _ACCOUNT_ID.fullmatch(input.account_id)
            or type(input.invoice_id) is not int
            or input.invoice_id <= 0
        ):
            return _branch("defect", FailureKind.VALIDATION, "FreshBooks account ID or invoice ID is invalid")
        path = f"/accounting/account/{input.account_id}/invoices/invoices/{input.invoice_id}"
        exchange = await self.client.get(call, path)
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        invoice = _decode_invoice(exchange.body, input)
        if invoice is None:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "FreshBooks returned invalid invoice JSON"
            )
        return QueryAttempt.branch_result("found", invoice, Receipt())
