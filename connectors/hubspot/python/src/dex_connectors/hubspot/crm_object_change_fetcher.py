# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The crmObjectChanged change fetcher: CRM search sorted by last-modified date, at parity with Go."""

import hashlib
import re
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from typing import Final

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)

from ._generated.definitions import CRM_OBJECT_CHANGED_TRIGGER_DEFINITION
from .client import Client
from .crm_object_change_cursor import (
    CRMObjectChangeCursor,
    decode_crm_object_change_cursor,
    encode_crm_object_change_cursor,
    lag_window_start,
    prune_seen,
)
from .models import (
    CRMObject,
    CRMObjectChangedEvent,
    CRMObjectChangedTriggerConfiguration,
    ObjectPage,
    SearchFilter,
    SearchFilterGroup,
    SearchObjectsInput,
    SearchSort,
)
from .operations import _page, _search_body

CRM_OBJECT_CHANGE_CREATED: Final = "created"
"""A record HubSpot created after the newest change an earlier poll of the binding observed."""
CRM_OBJECT_CHANGE_UPDATED: Final = "updated"
"""Any other change, including a record that appeared in search only after HubSpot's indexing delay."""
MAXIMUM_TRIGGER_PROPERTIES: Final = 98
"""Bounds the configured properties; the Trigger always also requests the creation and last-modified dates."""
MAXIMUM_SEARCH_RESULTS: Final = 10_000
"""HubSpot's limit on results reachable through paging for one search."""
CRM_OBJECT_CHANGE_PAGE_SIZE: Final = 100
"""Records per change search page; one page of full records stays well under the default response limit."""

_OPERATION: Final = "crmObjectChanged"
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_ZERO_TIME: Final = datetime.min.replace(tzinfo=UTC)
_RFC3339: Final = re.compile(
    r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))\Z",
    re.ASCII,
)


def last_modified_property(object_type: str) -> str:
    """Return the search property HubSpot updates on every change of an object type."""
    return "lastmodifieddate" if object_type == "contacts" else "hs_lastmodifieddate"


def new_crm_object_change_search(
    configuration: CRMObjectChangedTriggerConfiguration, anchor_milliseconds: int, after: str
) -> SearchObjectsInput:
    """Build the ascending last-modified search the Trigger repeats."""
    modified_property = last_modified_property(configuration.object_type)
    return SearchObjectsInput(
        configuration.object_type,
        filter_groups=(
            SearchFilterGroup(
                (SearchFilter(modified_property, operator="GTE", value=str(anchor_milliseconds)),)
            ),
        ),
        sort=SearchSort(modified_property, direction="ASCENDING"),
        properties=(*configuration.properties, "createdate", modified_property),
        limit=CRM_OBJECT_CHANGE_PAGE_SIZE,
        after=after,
    )


def validate_crm_object_changed_trigger_configuration(
    configuration: CRMObjectChangedTriggerConfiguration,
) -> None:
    """Reject a missing or unsupported object type, invalid property names, and an oversized search.

    The checks and messages are the Go connector's Validate; raises ValueError.
    """
    if not configuration.object_type:
        raise ValueError("crmObjectChanged objectType is required")
    if configuration.object_type not in ("contacts", "companies", "deals"):
        raise ValueError("crmObjectChanged object type must be contacts, companies, or deals")
    if len(configuration.properties) > MAXIMUM_TRIGGER_PROPERTIES:
        raise ValueError(f"crmObjectChanged allows at most {MAXIMUM_TRIGGER_PROPERTIES} properties")
    deepest_offset = str(MAXIMUM_SEARCH_RESULTS - CRM_OBJECT_CHANGE_PAGE_SIZE)
    try:
        _search_body(new_crm_object_change_search(configuration, 0, deepest_offset))
    except ValueError as error:
        raise ValueError(f"crmObjectChanged search: {error}") from error


def crm_object_change_id(object_type: str, object_id: str, modified_milliseconds: int) -> str:
    """Return the Trigger event ID, a pure function of the record version, as the Go connector does."""
    identity = "\x00".join((object_type, object_id, str(modified_milliseconds)))
    return "hubspot-" + hashlib.sha256(identity.encode()).digest()[:16].hex()


def parse_rfc3339_milliseconds(text: str) -> int | None:
    """Parse text as Go's time.RFC3339Nano does and return Unix milliseconds rounded down, or None."""
    match = _RFC3339.fullmatch(text)
    if match is None:
        return None
    year, month, day, hour, minute, second = (int(part) for part in match.groups()[:6])
    try:
        moment = datetime(year, month, day, hour, minute, second, tzinfo=UTC)
    except ValueError:
        return None
    offset_minutes = 0
    if match.group(8) != "Z":
        offset_hours, offset_minute = int(match.group(10)), int(match.group(11))
        if offset_hours > 24 or offset_minute > 60:
            return None
        offset_minutes = (offset_hours * 60 + offset_minute) * (-1 if match.group(9) == "-" else 1)
    whole_milliseconds = (moment - _UNIX_EPOCH) // timedelta(milliseconds=1)
    # Go reads at most nine fraction digits and ignores the rest.
    fraction_milliseconds = int((match.group(7) or "")[:9].ljust(9, "0")) // 1_000_000
    return whole_milliseconds - offset_minutes * 60_000 + fraction_milliseconds


def record_modified_milliseconds(record: CRMObject) -> int:
    """Read the last-modified property, falling back to the record's updatedAt; raises ValueError."""
    value = record.properties.get(last_modified_property(record.object_type))
    if value is not None:
        parsed = parse_rfc3339_milliseconds(value)
        if parsed is None:
            raise ValueError("HubSpot returned an invalid last-modified date")
        return parsed
    if record.updated_at == _ZERO_TIME:
        raise ValueError("HubSpot returned a record without a last-modified date")
    return (record.updated_at - _UNIX_EPOCH) // timedelta(milliseconds=1)


class CRMObjectChangeSearchError(Exception):
    """One unsuccessful change search exchange; its message carries no record data."""

    def __init__(self, failure: Failure) -> None:
        """Wrap the classified failure of one search."""
        super().__init__(f"HubSpot change search: {failure.message}")
        self.failure = failure
        """The classified HubSpot failure."""


class CRMObjectChangeFetcher:
    """Polls one object type through CRM search sorted by last-modified date.

    It implements the poll kit's ChangeFetcher; a poller calls it from one task at a time.
    """

    def __init__(
        self, client: Client, connection: ConnectionRef, configuration: CRMObjectChangedTriggerConfiguration
    ) -> None:
        """Bind the client, the connection whose credentials each poll resolves, and the configuration."""
        self._client = client
        self._call = new_trigger_credential_call(connection, CRM_OBJECT_CHANGED_TRIGGER_DEFINITION.trigger)
        self._configuration = configuration

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Read the newest last-modified date, so every existing record becomes the baseline.

        An object type without records seeds at zero, so every later record is delivered.
        """
        object_type = self._configuration.object_type
        modified_property = last_modified_property(object_type)
        newest = SearchObjectsInput(
            object_type,
            properties=(modified_property,),
            limit=1,
            sort=SearchSort(modified_property, direction="DESCENDING"),
        )
        page = await self._search(context, newest)
        floor = record_modified_milliseconds(page.objects[0]) if page.objects else 0
        return encode_crm_object_change_cursor(CRMObjectChangeCursor(object_type, floor, floor, floor + 1))

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[CRMObjectChangedEvent]]:
        """Read one search page after cursor.

        An undecodable cursor, a cursor for another object type, and a paging offset HubSpot rejects
        are marked invalid, so the kit records a new baseline.
        """
        try:
            position = decode_crm_object_change_cursor(cursor.token)
        except ValueError as error:
            raise mark_cursor_invalid(error) from error
        if position.object_type != self._configuration.object_type:
            raise mark_cursor_invalid(ValueError("HubSpot change cursor belongs to another object type"))
        search = new_crm_object_change_search(self._configuration, position.anchor_ms, position.after)
        try:
            page = await self._search(context, search)
        except CRMObjectChangeSearchError as error:
            if position.after and error.failure.kind is FailureKind.VALIDATION:
                rejected = ValueError(f"HubSpot rejected the change search offset: {error}")
                raise mark_cursor_invalid(rejected) from error
            raise
        next_position = replace(position, seen=dict(position.seen))
        changes: list[TriggerEvent[CRMObjectChangedEvent]] = []
        page_newest = position.anchor_ms
        for record in page.objects:
            modified = record_modified_milliseconds(record)
            page_newest = max(page_newest, modified)
            delivered = next_position.seen.get(record.id)
            if (delivered is not None and delivered >= modified) or modified <= position.floor_ms:
                continue
            created = (
                record.created_at != _ZERO_TIME
                and (record.created_at - _UNIX_EPOCH) // timedelta(milliseconds=1) > position.high_water_ms
            )
            changes.append(_change_event(record, modified, created))
            next_position.seen[record.id] = modified
            next_position.high_water_ms = max(next_position.high_water_ms, modified)
        prune_seen(next_position)
        has_more_pages = False
        if page.next_after and page_newest > position.anchor_ms:
            # Re-anchoring beats offset paging: an edit moves a record to the end and would shift the offsets.
            next_position.anchor_ms, next_position.after, has_more_pages = page_newest, "", True
        elif page.next_after:
            if int(page.next_after) + CRM_OBJECT_CHANGE_PAGE_SIZE > MAXIMUM_SEARCH_RESULTS:
                raise mark_cursor_invalid(
                    ValueError(
                        f"more than {MAXIMUM_SEARCH_RESULTS} HubSpot records share one last-modified time"
                    )
                )
            next_position.after, has_more_pages = page.next_after, True
        else:
            next_position.anchor_ms, next_position.after = lag_window_start(next_position), ""
        return ChangePage(changes, encode_crm_object_change_cursor(next_position), has_more_pages)

    async def _search(self, context: TriggerContext, search: SearchObjectsInput) -> ObjectPage:
        """Send one CRM search; 429, 423, and the daily limit pause the whole connection."""
        try:
            body = _search_body(search)
        except ValueError as error:
            raise ValueError(f"hubspot crmObjectChanged configuration: {error}") from error
        await wait_for_poll_request(context)
        result = await self._client.exchange(
            self._call, _OPERATION, "POST", f"crm/objects/2026-09/{search.object_type}/search", body=body
        )
        if result.disposition:
            failure = result.failure or Failure(FailureKind.PROTOCOL, "hubspot", _OPERATION, "unclassified")
            search_error = CRMObjectChangeSearchError(failure)
            if failure.kind is FailureKind.RATE_LIMIT:
                raise mark_rate_limited(search_error, timedelta(seconds=result.retry_after)) from None
            if failure.kind is FailureKind.QUOTA_EXHAUSTED:
                # The daily limit resets at midnight in the account's time zone; back off with no wait.
                raise mark_rate_limited(search_error, timedelta(0)) from None
            raise search_error
        try:
            page = _page(search.object_type, result.body)
        except (ValueError, TypeError, OverflowError) as error:
            raise ValueError(f"HubSpot change search returned an invalid page: {error}") from error
        if len(page.objects) > search.limit:
            raise ValueError(
                f"HubSpot change search returned {len(page.objects)} records, more than {search.limit}"
            )
        return page


def _change_event(record: CRMObject, modified: int, created: bool) -> TriggerEvent[CRMObjectChangedEvent]:
    modified_at = _UNIX_EPOCH + timedelta(milliseconds=modified)
    kind = CRM_OBJECT_CHANGE_CREATED if created else CRM_OBJECT_CHANGE_UPDATED
    return TriggerEvent(
        crm_object_change_id(record.object_type, record.id, modified),
        modified_at,
        CRMObjectChangedEvent(record.object_type, kind, modified_at, object=record),
    )
