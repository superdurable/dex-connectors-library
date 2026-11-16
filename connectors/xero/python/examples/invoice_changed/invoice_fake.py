# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""An in-process Xero organisation serving GET /Invoices and GET /Invoices/{id} for invoiceChanged tests.

It is a port of the Go connector's xeroinvoicefake: it honours If-Modified-Since to the second, the
Statuses filter, a Type where filter, order by UpdatedDateUTC, page and pageSize, and answers with
DateTimeUTC and a pagination object, as Xero's Accounting API does.
"""

import json
import threading
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta
from decimal import Decimal
from typing import Any
from urllib.parse import parse_qs

import httpx

INVOICES_PATH = "/api.xro/2.0/Invoices"
CONTACT_ID = "025867f1-d741-4d6b-b1af-9ac774b59ba7"
BASE_TIME_MILLISECONDS = int(datetime(2026, 10, 1, 9, 0, tzinfo=UTC).timestamp() * 1000)
"""The fake organisation's clock at start, 2026-10-01T09:00:00Z."""
DEFAULT_CHANGE_STEP_MILLISECONDS = 1250
"""Spaces changes by more than a second, so most changes fall in distinct seconds."""


@dataclass(frozen=True, slots=True)
class Request:
    """One recorded request."""

    path: str
    query: dict[str, list[str]]
    modified_since: str
    status: int


@dataclass(frozen=True, slots=True)
class _StoredInvoice:
    invoice_id: str
    number: str
    kind: str
    status: str
    total: str
    updated_at: int


def _encode(value: Any) -> str:
    """Encode JSON as Go's json.Marshal does for these maps: sorted keys, Decimals as raw numbers."""
    if isinstance(value, Decimal):
        return str(value)
    if isinstance(value, dict):
        members = (json.dumps(key) + ":" + _encode(value[key]) for key in sorted(value))
        return "{" + ",".join(members) + "}"
    if isinstance(value, list):
        return "[" + ",".join(_encode(item) for item in value) + "]"
    return json.dumps(value, ensure_ascii=False)


def _dotnet_date(milliseconds: int) -> str:
    return f"/Date({milliseconds}+0000)/"


def _at_least_one(values: list[str]) -> int:
    try:
        parsed = int(values[0]) if values else 0
    except ValueError:
        return 1
    return max(parsed, 1)


def _invoice_json(stored: _StoredInvoice) -> dict[str, Any]:
    amount_due, amount_paid = stored.total, "0.00"
    if stored.status == "PAID":
        amount_due, amount_paid = "0.00", stored.total
    return {
        "InvoiceID": stored.invoice_id,
        "InvoiceNumber": stored.number,
        "Type": stored.kind,
        "Status": stored.status,
        "Contact": {"ContactID": CONTACT_ID, "Name": "City Agency"},
        "DateString": "2026-10-01T00:00:00",
        "CurrencyCode": "USD",
        "LineAmountTypes": "NoTax",
        "LineItems": [
            {
                "Description": "Spring bouquet",
                "Quantity": Decimal("1.0000"),
                "UnitAmount": Decimal(stored.total),
                "AccountCode": "200",
                "LineAmount": Decimal(stored.total),
            }
        ],
        "SubTotal": Decimal(stored.total),
        "Total": Decimal(stored.total),
        "AmountDue": Decimal(amount_due),
        "AmountPaid": Decimal(amount_paid),
        "UpdatedDateUTC": _dotnet_date(stored.updated_at),
    }


class XeroInvoiceFake:
    """A stateful single-organisation fake that is safe to use from several threads."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._now = BASE_TIME_MILLISECONDS
        self._invoices: dict[str, _StoredInvoice] = {}
        self._rate_limited = 0
        self._rate_limit_kind = ""
        self._retry_after = ""
        self._failures = 0
        self._failure_status = 0
        self._requests: list[Request] = []
        self._maximum_per_page = 100

    def upsert(
        self,
        invoice_id: str,
        invoice_number: str,
        invoice_type: str,
        status: str,
        total: str,
        step_milliseconds: int = DEFAULT_CHANGE_STEP_MILLISECONDS,
    ) -> int:
        """Create or change an invoice stamped with the clock advanced by step; return its instant.

        invoice_type is ACCREC or ACCPAY and total a decimal string such as 49.95; a zero step stamps the
        same instant as the last change.
        """
        with self._lock:
            self._now += step_milliseconds
            key = invoice_id.lower()
            self._invoices[key] = _StoredInvoice(key, invoice_number, invoice_type, status, total, self._now)
            return self._now

    def set_status(self, invoice_id: str, status: str) -> int:
        """Change an existing invoice's status and stamp it as upsert does."""
        with self._lock:
            stored = self._invoices[invoice_id.lower()]
        return self.upsert(stored.invoice_id, stored.number, stored.kind, status, stored.total)

    def advance_clock(self, milliseconds: int) -> None:
        """Move the organisation's clock without changing an invoice."""
        with self._lock:
            self._now += milliseconds

    def rate_limit_next(self, count: int, problem: str, retry_after: str) -> None:
        """Answer the next count list requests with 429, X-Rate-Limit-Problem, and Retry-After."""
        with self._lock:
            self._rate_limited, self._rate_limit_kind, self._retry_after = count, problem, retry_after

    def fail_next(self, count: int, status: int) -> None:
        """Answer the next count list requests with status, such as 500 or 403."""
        with self._lock:
            self._failures, self._failure_status = count, status

    def limit_page_size(self, page_size: int) -> None:
        """Answer at most page_size invoices per page."""
        with self._lock:
            self._maximum_per_page = page_size

    def requests(self) -> list[Request]:
        """Return a copy of every recorded request."""
        with self._lock:
            return list(self._requests)

    def list_requests(self) -> list[Request]:
        """Return the recorded GET /Invoices list requests."""
        return [request for request in self.requests() if request.path == INVOICES_PATH]

    def transport(self) -> httpx.MockTransport:
        """Serve this organisation to an httpx client, whatever host the request names."""
        return httpx.MockTransport(self.handle)

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one request and record it."""
        with self._lock:
            recorded = Request(
                request.url.path,
                parse_qs(request.url.query.decode(), keep_blank_values=True),
                request.headers.get("If-Modified-Since", ""),
                0,
            )
            headers: dict[str, str] = {"Content-Type": "application/json; charset=utf-8"}
            status, body = self._answer(request, recorded, headers)
            self._requests.append(replace(recorded, status=status))
        return httpx.Response(status, headers=headers, content=body.encode())

    def _answer(self, request: httpx.Request, recorded: Request, headers: dict[str, str]) -> tuple[int, str]:
        if request.method != "GET" or not request.headers.get("Authorization", "").startswith("Bearer "):
            return 401, '{"Type":"AuthenticationUnsuccessful"}'
        if recorded.path == INVOICES_PATH:
            if self._rate_limited > 0:
                self._rate_limited -= 1
                headers["X-Rate-Limit-Problem"] = self._rate_limit_kind
                headers["Retry-After"] = self._retry_after
                return 429, '{"Type":"RateLimitExceeded"}'
            if self._failures > 0:
                self._failures -= 1
                return self._failure_status, '{"Type":"InjectedFailure"}'
            return self._list_invoices(recorded)
        if recorded.path.startswith(INVOICES_PATH + "/"):
            stored = self._invoices.get(recorded.path.removeprefix(INVOICES_PATH + "/").lower())
            if stored is None:
                return 404, '{"Type":"NotFound"}'
            return 200, _encode({"Invoices": [_invoice_json(stored)]})
        return 404, '{"Type":"NotFound"}'

    def _list_invoices(self, recorded: Request) -> tuple[int, str]:
        since = 0
        if recorded.modified_since:
            try:
                parsed = datetime.strptime(recorded.modified_since, "%Y-%m-%dT%H:%M:%S").replace(tzinfo=UTC)
            except ValueError:
                return 400, '{"Type":"ValidationException"}'
            since = (parsed - datetime(1970, 1, 1, tzinfo=UTC)) // timedelta(milliseconds=1)
        query = recorded.query
        statuses = {status for status in (query.get("Statuses") or [""])[0].split(",") if status}
        invoice_type = ""
        where = (query.get("where") or [""])[0]
        if where:
            if not where.startswith('Type=="') or not where.endswith('"'):
                return 400, '{"Type":"ValidationException"}'
            invoice_type = where.removeprefix('Type=="').removesuffix('"')
        matching = [
            stored
            for stored in self._invoices.values()
            # Xero compares If-Modified-Since to the second, inclusively.
            if stored.updated_at // 1000 * 1000 >= since
            and (not statuses or stored.status in statuses)
            and (not invoice_type or stored.kind == invoice_type)
        ]
        descending = (query.get("order") or [""])[0].endswith(" DESC")
        matching.sort(key=lambda stored: stored.invoice_id)
        matching.sort(key=lambda stored: stored.updated_at, reverse=descending)
        page = _at_least_one(query.get("page") or [])
        page_size = min(_at_least_one(query.get("pageSize") or []), self._maximum_per_page)
        start = min((page - 1) * page_size, len(matching))
        end = min(start + page_size, len(matching))
        return 200, _encode(
            {
                "DateTimeUTC": _dotnet_date(self._now),
                "pagination": {
                    "page": page,
                    "pageSize": page_size,
                    "pageCount": (len(matching) + page_size - 1) // page_size,
                    "itemCount": len(matching),
                },
                "Invoices": [_invoice_json(stored) for stored in matching[start:end]],
            }
        )
