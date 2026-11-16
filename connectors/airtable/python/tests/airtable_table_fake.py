# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One in-memory Airtable table served through an httpx mock transport for the recordChanged tests.

It mirrors the Go connector's internal/airtablefake: the base schema, and POST listRecords with the Last
modified time filter, sort, page size, and offset the Trigger sends, and GET of one record by ID for the
example Flow's read-back. Offsets name the next record and work only with their own query, as Airtable's
do. It never echoes the personal access token.
"""

import json
import re
import threading
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import Any

import httpx

BASE_ID = "appIntakeBase0001"
TABLE_ID = "tblIntakeLeads001"
FIELD_ID = "fldLastModified01"
ACCESS_TOKEN = "patFAKE.0123456789abcdef"
ENDPOINT = "https://airtable.fake.test"
_FILTER = re.compile(r'IS_AFTER\(\{(fld[0-9A-Za-z]{14})\}, DATETIME_PARSE\("([^"]+)"\)\)')


@dataclass(frozen=True, slots=True)
class FakeRecord:
    """One fake record; a None modified_at leaves the Last modified time field empty."""

    id: str
    created_at: datetime
    modified_at: datetime | None
    fields: dict[str, Any] = field(default_factory=dict)


@dataclass(frozen=True, slots=True)
class ScriptedResponse:
    """Replaces the next request's response."""

    status: int
    retry_after: str = ""
    error_type: str = ""


def fake_record(record_id: str, modified_at: datetime) -> FakeRecord:
    """Return a record created and last modified at modified_at, with an Email field."""
    return FakeRecord(record_id, modified_at, modified_at, {"Email": record_id + "@example.test"})


def format_milliseconds(at: datetime) -> str:
    """Format as Airtable does: UTC with milliseconds."""
    return at.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.") + f"{at.microsecond // 1000:03d}Z"


class AirtableTableFake:
    """The fake provider; safe to call from the Worker and test threads."""

    def __init__(self, *records: FakeRecord) -> None:
        self._lock = threading.Lock()
        self._field_name = "Last Modified"
        self._field_type = "lastModifiedTime"
        self._records = {record.id: record for record in records}
        self._scripted: list[ScriptedResponse] = []
        self._iterators: dict[str, str] = {}
        self.list_requests: list[dict[str, Any]] = []
        self.schema_requests = 0
        self.completed_requests = 0
        self.statuses: list[int] = []

    def http_client(self) -> httpx.AsyncClient:
        """Return a client whose requests this fake answers; the caller closes it."""
        return httpx.AsyncClient(transport=httpx.MockTransport(self.handle))

    def put(self, *records: FakeRecord) -> None:
        """Create or replace records, as an edit in Airtable would."""
        with self._lock:
            self._records.update({record.id: record for record in records})

    def rename_field(self, name: str) -> None:
        """Rename the Last modified time field; its ID stays the same."""
        with self._lock:
            self._field_name = name

    def set_field_type(self, field_type: str) -> None:
        """Change the watched field's type in the schema."""
        with self._lock:
            self._field_type = field_type

    def expire_iterators(self) -> None:
        """Forget every offset handed out, as an Airtable iterator timeout does."""
        with self._lock:
            self._iterators.clear()

    def script(self, *responses: ScriptedResponse) -> None:
        """Queue responses that replace the next requests' responses, in order."""
        with self._lock:
            self._scripted.extend(responses)

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one request."""
        with self._lock:
            response = self._answer(request)
            self.completed_requests += 1
            self.statuses.append(response.status_code)
            return response

    def _answer(self, request: httpx.Request) -> httpx.Response:
        if request.headers.get("Authorization") != "Bearer " + ACCESS_TOKEN:
            return _error(401, "AUTHENTICATION_REQUIRED")
        if self._scripted:
            scripted = self._scripted.pop(0)
            headers = {"Retry-After": scripted.retry_after} if scripted.retry_after else {}
            return _error(scripted.status, scripted.error_type, headers)
        path = request.url.path
        if request.method == "GET" and path == f"/v0/meta/bases/{BASE_ID}/tables":
            self.schema_requests += 1
            options = {"isValid": True, "referencedFieldIds": None, "result": {"type": "dateTime"}}
            fields = [
                {"id": "fldLeadEmail00001", "name": "Email", "type": "email"},
                {"id": FIELD_ID, "name": self._field_name, "type": self._field_type, "options": options},
            ]
            table = {"id": TABLE_ID, "name": "Leads", "primaryFieldId": "fldLeadEmail00001", "fields": fields}
            return httpx.Response(200, json={"tables": [table]})
        if request.method == "POST" and path == f"/v0/{BASE_ID}/{TABLE_ID}/listRecords":
            return self._list(json.loads(request.content))
        record_prefix = f"/v0/{BASE_ID}/{TABLE_ID}/"
        if request.method == "GET" and path.startswith(record_prefix + "rec"):
            record = self._records.get(path.removeprefix(record_prefix))
            if record is None:
                return _error(404, "NOT_FOUND")
            return httpx.Response(200, json=self._provider_record(record))
        return _error(404, "NOT_FOUND")

    def _list(self, body: dict[str, Any]) -> httpx.Response:
        page_size = body.get("pageSize")
        if not isinstance(page_size, int) or not 1 <= page_size <= 100:
            return _error(422, "INVALID_REQUEST_UNKNOWN")
        self.list_requests.append(body)
        match = _FILTER.fullmatch(body.get("filterByFormula", ""))
        sort = body.get("sort") or []
        if match is None or match.group(1) != FIELD_ID or len(sort) != 1 or sort[0]["field"] != FIELD_ID:
            return _error(422, "INVALID_FILTER_BY_FORMULA")
        since = datetime.fromisoformat(match.group(2))
        direction = sort[0].get("direction", "asc")
        matched = [r for r in self._records.values() if r.modified_at is not None and r.modified_at > since]
        # Airtable leaves ties unordered; record ID descending exposes that.
        matched.sort(key=lambda record: record.id, reverse=True)
        matched.sort(key=lambda record: record.modified_at or since, reverse=direction == "desc")
        query = body["filterByFormula"] + "|" + direction
        start = 0
        offset = body.get("offset", "")
        if offset:
            _, separator, next_record_id = offset.partition("/")
            positions = [index for index, record in enumerate(matched) if record.id == next_record_id]
            if not separator or self._iterators.get(offset) != query or not positions:
                return _error(422, "LIST_RECORDS_ITERATOR_NOT_AVAILABLE")
            start = positions[0]
        end = min(start + page_size, len(matched))
        records = [self._provider_record(record) for record in matched[start:end]]
        document: dict[str, Any] = {"records": records}
        if end < len(matched):
            next_offset = f"itr{len(self._iterators)}/{matched[end].id}"
            self._iterators[next_offset] = query
            document["offset"] = next_offset
        return httpx.Response(200, json=document)

    def _provider_record(self, record: FakeRecord) -> dict[str, Any]:
        fields = dict(record.fields)
        if record.modified_at is not None:
            fields[self._field_name] = format_milliseconds(record.modified_at)
        return {"id": record.id, "createdTime": format_milliseconds(record.created_at), "fields": fields}


def _error(status: int, error_type: str, headers: dict[str, str] | None = None) -> httpx.Response:
    message = {"error": {"type": error_type, "message": "SENTINEL provider message text"}}
    return httpx.Response(status, json=message, headers=headers)
