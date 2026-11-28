# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""FreshBooks invoice decisions matching the Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any, TypeVar

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import GetInvoiceInput, Invoice, InvoicePage, ListInvoicesInput

_ACCOUNT_ID = re.compile(r"^[A-Za-z0-9_-]{1,64}$")
_AMOUNT = re.compile(r"^-?[0-9]+(?:\.[0-9]+)?$")
_CURRENCY = re.compile(r"^[A-Z]{3}$")


_Output = TypeVar("_Output", Invoice, InvoicePage)


def _read_failure(exchange: Exchange, operation: str, empty: _Output) -> QueryAttempt[_Output] | None:
    def branch(branch_id: str, kind: FailureKind, message: str) -> QueryAttempt[_Output]:
        return QueryAttempt.branch_result(
            branch_id, empty, Receipt(), Failure(kind, "freshbooks", operation, message)
        )

    def retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[_Output]:
        return QueryAttempt.retry(Failure(kind, "freshbooks", operation, message), delay)

    if exchange.error == "credential":
        return branch(
            "defect", FailureKind.AUTHENTICATION, "FreshBooks OAuth credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return retry(FailureKind.TRANSPORT, "FreshBooks request failed")
    if exchange.status == 429:
        return retry(FailureKind.RATE_LIMIT, "FreshBooks rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return retry(FailureKind.AVAILABILITY, "FreshBooks is temporarily unavailable", exchange.retry_after)
    if exchange.error == "too_large":
        return branch(
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "FreshBooks response exceeded the configured limit",
        )
    if exchange.error:
        return retry(FailureKind.TRANSPORT, "FreshBooks response was interrupted")
    if exchange.status == 404:
        if operation == "listInvoices":
            return branch("missing", FailureKind.NOT_FOUND, "FreshBooks did not find the account")
        return QueryAttempt.branch_result("missing", empty, Receipt())
    if exchange.status != 200:
        return branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"FreshBooks rejected HTTP {exchange.status}"
        )
    return None


def _result(body: bytes) -> dict[str, Any]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return {}
    if not isinstance(document, dict):
        return {}
    response = document.get("response")
    result = response.get("result") if isinstance(response, dict) else None
    return result if isinstance(result, dict) else {}


def _decode_invoice(invoice: object, input: GetInvoiceInput) -> Invoice | None:
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
        or type(invoice_id) is not int
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
            return _invalid(
                "getInvoice",
                Invoice(),
                "defect",
                FailureKind.VALIDATION,
                "FreshBooks account ID or invoice ID is invalid",
            )
        path = f"/accounting/account/{input.account_id}/invoices/invoices/{input.invoice_id}"
        exchange = await self.client.get(call, path)
        failure = _read_failure(exchange, "getInvoice", Invoice())
        if failure is not None:
            return failure
        invoice = _decode_invoice(_result(exchange.body).get("invoice"), input)
        if invoice is None:
            return _invalid(
                "getInvoice",
                Invoice(),
                "invalidResponse",
                FailureKind.PROTOCOL,
                "FreshBooks returned invalid invoice JSON",
            )
        return QueryAttempt.branch_result("found", invoice, Receipt())


def _invalid(
    operation: str, empty: _Output, branch: str, kind: FailureKind, message: str
) -> QueryAttempt[_Output]:
    return QueryAttempt.branch_result(
        branch, empty, Receipt(), Failure(kind, "freshbooks", operation, message)
    )


class ListInvoicesOperation:
    """Read one page of invoices from a FreshBooks account with its existing OAuth grant."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated listInvoices Query contract."""
        return definitions.LIST_INVOICES_DEFINITION

    async def invoke(self, call: Call, input: ListInvoicesInput) -> QueryAttempt[InvoicePage]:
        """Read up to 100 invoices and return pagination metadata in provider order."""
        page = input.page or 1
        empty = InvoicePage()
        if (
            not _ACCOUNT_ID.fullmatch(input.account_id)
            or type(input.page) is not int
            or not 1 <= page <= 10000
        ):
            return _invalid(
                "listInvoices",
                empty,
                "defect",
                FailureKind.VALIDATION,
                "FreshBooks account ID or page is invalid",
            )
        exchange = await self.client.get(
            call, f"/accounting/account/{input.account_id}/invoices/invoices?page={page}&per_page=100"
        )
        failure = _read_failure(exchange, "listInvoices", empty)
        if failure is not None:
            return failure
        result = _result(exchange.body)
        invoices = _invoice_page(result, input.account_id, page)
        if invoices is None:
            return _invalid(
                "listInvoices",
                empty,
                "invalidResponse",
                FailureKind.PROTOCOL,
                "FreshBooks returned invalid invoice page JSON",
            )
        return QueryAttempt.branch_result("listed", invoices, Receipt())


def _invoice_page(result: dict[str, Any], account_id: str, page: int) -> InvoicePage | None:
    rows, actual_page = result.get("invoices"), result.get("page")
    pages, total = result.get("pages", 0), result.get("total", 0)
    if (
        not isinstance(rows, list)
        or type(actual_page) is not int
        or actual_page != page
        or type(pages) is not int
        or pages < 0
        or type(total) is not int
        or total < 0
    ):
        return None
    invoices = []
    for row in rows:
        invoice_id = row.get("invoiceid") if isinstance(row, dict) else None
        if type(invoice_id) is not int or not 0 < invoice_id <= (1 << 63) - 1:
            return None
        invoice = _decode_invoice(row, GetInvoiceInput(account_id, invoice_id))
        if invoice is None:
            return None
        invoices.append(invoice)
    return InvoicePage(account_id, tuple(invoices), page, pages, total)
