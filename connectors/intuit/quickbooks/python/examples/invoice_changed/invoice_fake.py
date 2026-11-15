# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""An in-process stand-in for the QuickBooks Online invoice query and read endpoints.

It mirrors the Go connector's internal/quickbooksinvoicefake: a company clock, a new SyncToken and
LastUpdatedTime on every write, the watermark query answered in LastUpdatedTime order, and throttling.
"""

import json
import re
import threading
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta, timezone

import httpx

# QuickBooks writes MetaData times in company time.
COMPANY_ZONE = timezone(timedelta(hours=-7))
_COMPANY_PATH = re.compile(r"/v3/company/([0-9]+)/(query|invoice/([0-9]+))")
_QUERY = re.compile(
    r"select \* from Invoice(?: where MetaData\.LastUpdatedTime >= '([^']+)')? ORDERBY "
    r"MetaData\.LastUpdatedTime( DESC)? STARTPOSITION ([0-9]+) MAXRESULTS ([0-9]+)"
)


@dataclass
class _StoredInvoice:
    invoice_id: str
    doc_number: str
    customer_id: str
    amount: str
    balance: str
    sync_token: int
    created_at: datetime
    updated_at: datetime

    def wire(self) -> str:
        """Encode the invoice as QuickBooks does, keeping exact decimal amounts as JSON numbers."""
        created = self.created_at.isoformat()
        return (
            "{"
            f'"Balance":{self.balance},"CustomerRef":{{"value":{json.dumps(self.customer_id)}}},'
            f'"DocNumber":{json.dumps(self.doc_number)},"EmailStatus":"NotSet",'
            f'"Id":{json.dumps(self.invoice_id)},"Line":[{{"Amount":{self.amount},'
            '"DetailType":"SalesItemLineDetail","Id":"1","SalesItemLineDetail":{"ItemRef":{"value":"1"},'
            f'"Qty":1,"UnitPrice":{self.amount}}}}},{{"Amount":{self.amount},'
            '"DetailType":"SubTotalLineDetail","SubTotalLineDetail":{}}],'
            f'"MetaData":{{"CreateTime":"{created}","LastUpdatedTime":"{self.updated_at.isoformat()}"}},'
            f'"SyncToken":"{self.sync_token}","TotalAmt":{self.amount},"TxnDate":"{created[:10]}"'
            "}"
        )


def _fault(status: int, fault_type: str, code: str, headers: dict[str, str] | None = None) -> httpx.Response:
    body = {"Fault": {"Error": [{"Message": "fake QuickBooks fault", "code": code}], "type": fault_type}}
    return httpx.Response(status, headers=headers or {}, json=body)


class QuickBooksInvoiceFake:
    """One fake company, safe for use by the poller and the test at once."""

    def __init__(self, realm_id: str) -> None:
        """Start an empty company whose clock reads 2026-11-02T09:00:00-07:00."""
        self.realm_id = realm_id
        self._lock = threading.Lock()
        self._clock = datetime(2026, 11, 2, 9, 0, 0, tzinfo=COMPANY_ZONE)
        self._next_id = 130
        self._invoices: dict[str, _StoredInvoice] = {}
        self._throttles = 0
        self._retry_after = ""
        self._queries: list[str] = []
        self._rejects_watermarks = False
        self._is_clock_held = False

    def transport(self) -> httpx.MockTransport:
        """Return an httpx transport that answers every request in process."""
        return httpx.MockTransport(self.handle)

    def create(self, doc_number: str, customer_id: str, amount: str) -> str:
        """Add an invoice at SyncToken 0, one company second after the previous write; return its Id."""
        with self._lock:
            return self._add(doc_number, customer_id, amount, self._tick())

    def create_backdated(self, doc_number: str, customer_id: str, amount: str, age: timedelta) -> str:
        """Add an invoice stamped age before the company clock, as a write made visible late."""
        with self._lock:
            return self._add(doc_number, customer_id, amount, self._clock - age)

    def pay(self, invoice_id: str) -> None:
        """Zero the balance as an applied payment does, bumping the SyncToken."""
        with self._lock:
            changed = self._invoices[invoice_id]
            changed.balance, changed.sync_token, changed.updated_at = (
                "0",
                changed.sync_token + 1,
                self._tick(),
            )

    def hold_clock(self) -> None:
        """Make later writes share the next company second until `release_clock`, as a burst does."""
        with self._lock:
            self._clock += timedelta(seconds=1)
            self._is_clock_held = True

    def release_clock(self) -> None:
        """Let every later write advance the company clock by one second again."""
        with self._lock:
            self._is_clock_held = False

    def throttle(self, count: int, retry_after: str) -> None:
        """Answer the next count queries with HTTP 429; blank retry_after sends no Retry-After header."""
        with self._lock:
            self._throttles, self._retry_after = count, retry_after

    def reject_watermark_queries(self, rejects: bool) -> None:
        """Answer every later watermark query with HTTP 400 ValidationFault; the baseline still succeeds."""
        with self._lock:
            self._rejects_watermarks = rejects

    def queries(self) -> list[str]:
        """Return every query statement received, in order."""
        with self._lock:
            return list(self._queries)

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Serve GET /v3/company/{realm}/query and GET /v3/company/{realm}/invoice/{id}."""
        match = _COMPANY_PATH.fullmatch(request.url.path)
        if not request.headers.get("Authorization"):
            return _fault(401, "AuthenticationFault", "3200")
        if request.method != "GET" or match is None:
            return _fault(400, "ValidationFault", "4000")
        if match.group(1) != self.realm_id:
            return _fault(403, "AuthorizationFault", "3100")
        if match.group(3):
            return self._serve_invoice(match.group(3))
        return self._serve_query(request.url.params.get("query", ""))

    def _serve_invoice(self, invoice_id: str) -> httpx.Response:
        with self._lock:
            found = self._invoices.get(invoice_id)
            if found is None:
                return _fault(400, "ValidationFault", "610")
            body = '{"Invoice":' + found.wire() + ',"time":"' + self._clock.isoformat() + '"}'
        return httpx.Response(200, headers={"Content-Type": "application/json"}, content=body.encode())

    def _serve_query(self, statement: str) -> httpx.Response:
        with self._lock:
            self._queries.append(statement)
            if self._throttles > 0:
                self._throttles -= 1
                headers = {"Retry-After": self._retry_after} if self._retry_after else {}
                return _fault(429, "ThrottleExceeded", "003001", headers)
            match = _QUERY.fullmatch(statement)
            if match is None or (self._rejects_watermarks and match.group(1)):
                return _fault(400, "ValidationFault", "4000")
            since_text, descending, start_text, maximum_text = match.groups()
            try:
                since = datetime.fromisoformat(since_text.replace("Z", "+00:00")) if since_text else None
            except ValueError:
                return _fault(400, "ValidationFault", "4000")
            selected = [
                stored for stored in self._invoices.values() if since is None or stored.updated_at >= since
            ]
            # Ties keep Id order, as QuickBooks keeps equal timestamps in a stable order.
            selected.sort(key=lambda stored: int(stored.invoice_id))
            selected.sort(key=lambda stored: stored.updated_at, reverse=bool(descending))
            start = int(start_text) - 1
            page = selected[start : start + int(maximum_text)]
        members = [f'"startPosition":{json.dumps(start_text)}', f'"maxResults":{len(page)}']
        if page:
            members.insert(0, '"Invoice":[' + ",".join(stored.wire() for stored in page) + "]")
        now = datetime.now(UTC).astimezone(COMPANY_ZONE).replace(microsecond=0).isoformat()
        body = '{"QueryResponse":{' + ",".join(members) + '},"time":"' + now + '"}'
        return httpx.Response(200, headers={"Content-Type": "application/json"}, content=body.encode())

    def _add(self, doc_number: str, customer_id: str, amount: str, stamped: datetime) -> str:
        self._next_id += 1
        invoice_id = str(self._next_id)
        self._invoices[invoice_id] = _StoredInvoice(
            invoice_id, doc_number, customer_id, amount, amount, 0, stamped, stamped
        )
        return invoice_id

    def _tick(self) -> datetime:
        if not self._is_clock_held:
            self._clock += timedelta(seconds=1)
        return self._clock
