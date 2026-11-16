# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A deterministic Salesforce REST query and record read stand-in for the recordModifiedPolled tests.

It mirrors the Go connector's internal/salesforcepollfake: it answers only the SOQL statements the
Trigger sends and stamps every change with a whole-second SystemModstamp that only moves forward.
Serve it through `httpx.MockTransport`.
"""

import json
import re
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from email.utils import format_datetime
from typing import Any

import httpx

DATA_PATH_PREFIX = "/services/data/v62.0"
"""The REST path the default connector configuration uses."""
INSTANCE_URL = "https://fake-org.my.salesforce.com"
"""The instance URL the fake answers."""

_SEED_QUERY = re.compile(
    r"SELECT Id, SystemModstamp FROM (\w+) WHERE SystemModstamp <= (\S+) "
    r"ORDER BY SystemModstamp DESC, Id DESC LIMIT 1\Z"
)
_CHANGES_QUERY = re.compile(
    r"SELECT (.+) FROM (\w+) WHERE SystemModstamp <= (\S+) AND (?:SystemModstamp > (\S+)|"
    r"\(SystemModstamp > (\S+) OR \(SystemModstamp = (\S+) AND Id > '(\w+)'\)\)) "
    r"ORDER BY SystemModstamp ASC, Id ASC LIMIT ([0-9]+)\Z"
)


@dataclass(slots=True)
class FakeRecord:
    """One stored record."""

    s_object_type: str
    id: str
    created: datetime
    modified: datetime
    modstamp: datetime
    fields: dict[str, Any]


@dataclass(frozen=True, slots=True)
class FakeRequest:
    """One recorded REST request."""

    path: str
    soql: str
    status: int


@dataclass
class SalesforceOrgFake:
    """A stateful single-org fake that accepts only access_token."""

    access_token: str
    clock: Callable[[], datetime] = field(default=lambda: datetime.now(UTC))
    clock_skew: timedelta = timedelta(0)
    records: dict[str, FakeRecord] = field(default_factory=dict)
    requests: list[FakeRequest] = field(default_factory=list)
    rejected_objects: dict[str, str] = field(default_factory=dict)
    rate_limited: int = 0
    retry_after: str = ""
    id_prefix: str = "006RM"
    delete_after_listing: set[str] = field(default_factory=set)
    """Record IDs deleted right after a change query lists them, as a deletion before the read-back."""
    _next_id: int = 1
    _last_stamp: datetime = field(init=False)

    def __post_init__(self) -> None:
        # Change stamps start one minute before the org's clock, so the settle delay never hides them.
        self._last_stamp = (self.clock().astimezone(UTC) - timedelta(minutes=1)).replace(microsecond=0)

    def transport(self) -> httpx.MockTransport:
        """Return a transport that answers every request from this fake."""
        return httpx.MockTransport(self.handle)

    def insert(self, s_object_type: str, fields: dict[str, Any]) -> str:
        """Create a record of s_object_type and return its 18-character ID."""
        return self._store(s_object_type, self._advance_stamp(), fields)

    def insert_batch(self, s_object_type: str, count: int) -> list[str]:
        """Create count records sharing one SystemModstamp, as a bulk load does."""
        stamp = self._advance_stamp()
        return [self._store(s_object_type, stamp, {}) for _ in range(count)]

    def insert_recent(self, s_object_type: str, fields: dict[str, Any]) -> str:
        """Create a record stamped at the org's current second, as a change committed just now."""
        stamp = self._server_now().replace(microsecond=0)
        if stamp <= self._last_stamp:
            stamp = self._last_stamp + timedelta(seconds=1)
        self._last_stamp = stamp
        return self._store(s_object_type, stamp, fields)

    def update(self, record_id: str, fields: dict[str, Any]) -> None:
        """Merge fields and move LastModifiedDate and SystemModstamp forward."""
        record = self.records[record_id]
        stamp = self._advance_stamp()
        record.modified = record.modstamp = stamp
        record.fields.update(fields)

    def delete(self, record_id: str) -> None:
        """Remove the record; a poll never reports it, and a read answers 404 NOT_FOUND."""
        del self.records[record_id]

    def rate_limit_next(self, count: int, retry_after: str) -> None:
        """Answer the next count requests with 403 REQUEST_LIMIT_EXCEEDED and retry_after, if set."""
        self.rate_limited, self.retry_after = count, retry_after

    @property
    def query_count(self) -> int:
        """Return how many query requests were served."""
        return sum(1 for request in self.requests if request.soql)

    def _store(self, s_object_type: str, stamp: datetime, fields: dict[str, Any]) -> str:
        record_id = f"{self.id_prefix}{self._next_id:010d}AAA"
        self._next_id += 1
        self.records[record_id] = FakeRecord(s_object_type, record_id, stamp, stamp, stamp, dict(fields))
        return record_id

    def _advance_stamp(self) -> datetime:
        stamp = self._last_stamp + timedelta(seconds=1)
        floor = (self._server_now() - timedelta(minutes=1)).replace(microsecond=0)
        self._last_stamp = max(stamp, floor)
        return self._last_stamp

    def _server_now(self) -> datetime:
        return self.clock().astimezone(UTC) + self.clock_skew

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one request and record it."""
        soql = request.url.params.get("q", "")
        status, body = self._answer(request, soql)
        self.requests.append(FakeRequest(request.url.path, soql, status))
        headers = {
            "Content-Type": "application/json",
            "Sforce-Limit-Info": "api-usage=25/15000",
            "Date": format_datetime(self._server_now(), usegmt=True),
        }
        if status == 403 and self.retry_after:
            headers["Retry-After"] = self.retry_after
        return httpx.Response(status, headers=headers, content=body)

    def _answer(self, request: httpx.Request, soql: str) -> tuple[int, bytes]:
        if request.headers.get("Authorization") != "Bearer " + self.access_token:
            return 401, _error_body("INVALID_SESSION_ID")
        if self.rate_limited > 0:
            self.rate_limited -= 1
            return 403, _error_body("REQUEST_LIMIT_EXCEEDED")
        path = request.url.path
        if path == DATA_PATH_PREFIX + "/query":
            for s_object_type, error_code in self.rejected_objects.items():
                if f" FROM {s_object_type} " in soql:
                    return 400, _error_body(error_code)
            return self._answer_query(soql)
        if path.startswith(DATA_PATH_PREFIX + "/sobjects/"):
            s_object_type, _, record_id = path.removeprefix(DATA_PATH_PREFIX + "/sobjects/").partition("/")
            record = self.records.get(record_id)
            if record is None or record.s_object_type != s_object_type:
                return 404, _error_body("NOT_FOUND")
            fields = request.url.params.get("fields", "").split(",")
            return 200, json.dumps(_record_object(record, fields)).encode()
        return 404, _error_body("NOT_FOUND")

    def _answer_query(self, soql: str) -> tuple[int, bytes]:
        seed = _SEED_QUERY.fullmatch(soql)
        if seed is not None:
            horizon = _parse_time(seed[2])
            matching = self._sorted(seed[1], lambda record: record.modstamp <= horizon)
            return 200, _query_body(matching[-1:], ["Id", "SystemModstamp"])
        changes = _CHANGES_QUERY.fullmatch(soql)
        if changes is None:
            return 400, _error_body("MALFORMED_QUERY")
        after_text, after_id = changes[4], ""
        if after_text is None:
            after_text, after_id = changes[5], changes[7]
            if changes[6] != changes[5]:
                return 400, _error_body("MALFORMED_QUERY")
        horizon, after, limit = _parse_time(changes[3]), _parse_time(after_text), int(changes[8])

        def is_matching(record: FakeRecord) -> bool:
            is_after = record.modstamp > after or (
                after_id != "" and record.modstamp == after and record.id > after_id
            )
            return is_after and record.modstamp <= horizon

        matching = self._sorted(changes[2], is_matching)[:limit]
        body = _query_body(matching, changes[1].split(", "))
        for record in matching:
            if record.id in self.delete_after_listing:
                self.delete_after_listing.discard(record.id)
                del self.records[record.id]
        return 200, body

    def _sorted(self, s_object_type: str, is_matching: Callable[[FakeRecord], bool]) -> list[FakeRecord]:
        matching = [
            record
            for record in self.records.values()
            if record.s_object_type == s_object_type and is_matching(record)
        ]
        return sorted(matching, key=lambda record: (record.modstamp, record.id))


def _parse_time(text: str) -> datetime:
    return datetime.fromisoformat(text.replace("Z", "+00:00"))


def _format_salesforce_time(moment: datetime) -> str:
    return moment.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.000+0000")


def _query_body(records: list[FakeRecord], fields: list[str]) -> bytes:
    objects = [_record_object(record, fields) for record in records]
    return json.dumps({"totalSize": len(objects), "done": True, "records": objects}).encode()


def _record_object(record: FakeRecord, fields: list[str]) -> dict[str, Any]:
    """Render the requested fields; a field the record never set is null, as Salesforce returns it."""
    url = f"{DATA_PATH_PREFIX}/sobjects/{record.s_object_type}/{record.id}"
    rendered: dict[str, Any] = {"attributes": {"type": record.s_object_type, "url": url}}
    for name in fields:
        if name == "Id":
            rendered[name] = record.id
        elif name == "CreatedDate":
            rendered[name] = _format_salesforce_time(record.created)
        elif name == "LastModifiedDate":
            rendered[name] = _format_salesforce_time(record.modified)
        elif name == "SystemModstamp":
            rendered[name] = _format_salesforce_time(record.modstamp)
        else:
            rendered[name] = record.fields.get(name)
    return rendered


def _error_body(error_code: str) -> bytes:
    return json.dumps([{"message": "fake Salesforce error", "errorCode": error_code, "fields": []}]).encode()
