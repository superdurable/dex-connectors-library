# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A deterministic HubSpot CRM search and record read stand-in for the crmObjectChanged tests.

It mirrors the Go connector's internal/hubspotsearchfake: its clock moves one second per change, so
modification times and Trigger event IDs are deterministic. Serve it through `httpx.MockTransport`.
"""

import json
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta

import httpx

ACCESS_TOKEN = "pat-na1-fake-hubspot-token"
"""The only bearer token the fake accepts."""
_OBJECTS_PATH_PREFIX = "/crm/objects/2026-09/"


@dataclass(slots=True)
class FakeRecord:
    """One stored CRM record; search omits it while it is not indexed."""

    id: str
    properties: dict[str, str]
    created_at: datetime
    modified_at: datetime
    is_indexed: bool


@dataclass(frozen=True, slots=True)
class FailedResponse:
    """One scripted error answer to the next search."""

    status: int
    body: str
    headers: dict[str, str] = field(default_factory=dict)


class HubSpotSearchFake:
    """Serves search and reads of one object type over an in-memory record set."""

    def __init__(self, object_type: str, start: datetime) -> None:
        """Start the clock at start, truncated to the millisecond."""
        self._object_type = object_type
        self._clock = start.astimezone(UTC).replace(microsecond=start.microsecond // 1000 * 1000)
        self._records: dict[str, FakeRecord] = {}
        self._next_id = 1000
        self._failures: list[FailedResponse] = []
        self.search_bodies: list[dict[str, object]] = []
        """The decoded search request bodies, in order."""

    @property
    def search_request_count(self) -> int:
        """Return the number of search requests received."""
        return len(self.search_bodies)

    def transport(self) -> httpx.MockTransport:
        """Return a transport that answers every request from this fake."""
        return httpx.MockTransport(self.handle)

    def create(self, properties: dict[str, str]) -> str:
        """Store a new indexed record and return its ID."""
        self._clock += timedelta(seconds=1)
        return self._store(properties, True)

    def create_unindexed(self, properties: dict[str, str]) -> str:
        """Store a record that search omits until `index`, as HubSpot does while it indexes."""
        self._clock += timedelta(seconds=1)
        return self._store(properties, False)

    def create_at_same_time(self, count: int, properties: dict[str, str]) -> list[str]:
        """Store count indexed records that share one modification time and return their IDs."""
        self._clock += timedelta(seconds=1)
        return [self._store(properties, True) for _ in range(count)]

    def update(self, record_id: str, properties: dict[str, str]) -> None:
        """Set properties on a record and move its modification time forward."""
        self._clock += timedelta(seconds=1)
        record = self._records[record_id]
        record.properties.update(properties)
        record.modified_at = self._clock

    def index(self, record_id: str) -> None:
        """Make an unindexed record visible to search."""
        self._records[record_id].is_indexed = True

    def fail_next_searches(self, *responses: FailedResponse) -> None:
        """Answer the next searches with responses, in order."""
        self._failures.extend(responses)

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one request."""
        if request.headers.get("Authorization") != f"Bearer {ACCESS_TOKEN}":
            return httpx.Response(401, json={"category": "INVALID_AUTHENTICATION"})
        prefix = f"{_OBJECTS_PATH_PREFIX}{self._object_type}/"
        if not request.url.path.startswith(prefix):
            return httpx.Response(404, json={"category": "OBJECT_NOT_FOUND"})
        suffix = request.url.path.removeprefix(prefix)
        if suffix == "search" and request.method == "POST":
            return self._search(json.loads(request.content))
        if request.method == "GET":
            names = request.url.params.get("properties", "")
            return self._read(suffix, names.split(",") if names else [])
        return httpx.Response(404, json={"category": "OBJECT_NOT_FOUND"})

    def _store(self, properties: dict[str, str], is_indexed: bool) -> str:
        self._next_id += 1
        record_id = str(self._next_id)
        self._records[record_id] = FakeRecord(
            record_id, dict(properties), self._clock, self._clock, is_indexed
        )
        return record_id

    def _modified_property(self) -> str:
        return "lastmodifieddate" if self._object_type == "contacts" else "hs_lastmodifieddate"

    def _search(self, search: dict[str, object]) -> httpx.Response:
        limit = search.get("limit", 0)
        sorts = search.get("sorts") or []
        if (
            not isinstance(limit, int)
            or not isinstance(sorts, list)
            or len(sorts) > 1
            or not 1 <= limit <= 200
        ):
            return httpx.Response(400, json={"category": "VALIDATION_ERROR"})
        self.search_bodies.append(search)
        if self._failures:
            failure = self._failures.pop(0)
            headers = {**failure.headers, "Content-Type": "application/json"}
            return httpx.Response(failure.status, headers=headers, content=failure.body.encode())
        offset = int(str(search.get("after") or "0"))
        if offset + limit > 10_000:
            return httpx.Response(400, json={"category": "VALIDATION_ERROR"})
        matched = [record for record in self._records.values() if self._matches(record, search)]
        is_descending = len(sorts) == 1 and sorts[0].get("direction") == "DESCENDING"
        matched.sort(
            key=lambda record: (record.modified_at, len(record.id), record.id), reverse=is_descending
        )
        names = search.get("properties") or []
        assert isinstance(names, list)
        results = [self._record_json(record, list(names)) for record in matched[offset : offset + limit]]
        response: dict[str, object] = {"total": len(matched), "results": results}
        if offset + limit < len(matched):
            response["paging"] = {"next": {"after": str(offset + limit)}}
        return httpx.Response(200, json=response)

    def _matches(self, record: FakeRecord, search: dict[str, object]) -> bool:
        if not record.is_indexed:
            return False
        groups = search.get("filterGroups") or []
        assert isinstance(groups, list)
        threshold_ms = (record.modified_at - datetime(1970, 1, 1, tzinfo=UTC)) // timedelta(milliseconds=1)
        for group in groups:
            for search_filter in group["filters"]:
                value = str(search_filter.get("value", ""))
                if (
                    search_filter.get("propertyName") != self._modified_property()
                    or search_filter.get("operator") != "GTE"
                    or not value.lstrip("-").isdigit()
                    or threshold_ms < int(value)
                ):
                    return False
        return True

    def _read(self, record_id: str, names: list[str]) -> httpx.Response:
        record = self._records.get(record_id)
        if record is None:
            return httpx.Response(404, json={"category": "OBJECT_NOT_FOUND"})
        return httpx.Response(200, json=self._record_json(record, names))

    def _record_json(self, record: FakeRecord, names: list[str]) -> dict[str, object]:
        """Return the requested properties, or every property when none are named."""
        every = {
            "hs_object_id": record.id,
            "createdate": _format_time(record.created_at),
            self._modified_property(): _format_time(record.modified_at),
            **record.properties,
        }
        properties = {name: value for name, value in every.items() if not names or name in names}
        return {
            "id": record.id,
            "properties": properties,
            "archived": False,
            "createdAt": _format_time(record.created_at),
            "updatedAt": _format_time(record.modified_at),
        }


def _format_time(value: datetime) -> str:
    return value.strftime("%Y-%m-%dT%H:%M:%S.") + f"{value.microsecond // 1000:03d}Z"
