# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The recordModifiedPolled change fetcher: SOQL ordered by (SystemModstamp, Id), at parity with Go."""

from datetime import UTC, datetime, timedelta
from email.utils import parsedate_to_datetime
from typing import Final

from dex_connectors.sdk import (
    ConnectionRef,
    FailureKind,
    ReauthorizationRequiredError,
    TriggerContext,
    TriggerEvent,
    resolve_credential,
)
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)

from ._generated.definitions import RECORD_MODIFIED_POLLED_TRIGGER_DEFINITION
from .client import Client, go_query, is_record_id
from .models import (
    QueryRecordsOutput,
    Record,
    RecordModifiedEvent,
    RecordModifiedTriggerConfiguration,
    SOQLValue,
)
from .operations import decode_query_page, render_soql
from .record_modified_trigger import (
    Instant,
    format_rfc3339,
    is_same_record_id,
    parse_salesforce_datetime,
    record_change_kind,
    record_modified_event_id,
    selected_fields,
    string_field,
)
from .record_poll_cursor import RecordPollCursor, decode_record_poll_cursor, encode_record_poll_cursor

MODSTAMP_SETTLE_DELAY: Final = timedelta(seconds=15)
"""Leaves recent SystemModstamp values unread, so a transaction committing after its stamp is still read."""
_SERVER_CLOCK_TOLERANCE: Final = timedelta(seconds=1)
"""Absorbs the HTTP Date header's whole-second truncation."""
_OPERATION: Final = "recordModifiedPolled"
_MAX_ENCODED_QUERY_BYTES: Final = 15000
_MINIMUM_QUERY_BATCH_SIZE: Final = 200


class RecordPollError(Exception):
    """One unsuccessful recordModifiedPolled poll; its message carries no record values or credentials."""


class RecordPollFetcher:
    """Reads one object's records in (SystemModstamp, Id) order for the poll Trigger kit.

    It implements the kit's ChangeFetcher; a poller calls it from one task at a time.
    """

    def __init__(
        self, client: Client, connection: ConnectionRef, configuration: RecordModifiedTriggerConfiguration
    ) -> None:
        """Bind the client, the connection whose credentials each poll resolves, and the configuration."""
        self._client = client
        self._call = new_trigger_credential_call(
            connection, RECORD_MODIFIED_POLLED_TRIGGER_DEFINITION.trigger
        )
        self._configuration = configuration
        self._server_clock_offset = timedelta(0)

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Position the cursor at the newest record stamped before the settle horizon, or at the horizon.

        Existing records become the baseline and are never delivered.
        """
        horizon = self._settle_horizon()
        cursor = await self._seed_cursor_at(context, horizon)
        # A Salesforce clock behind this host's must not seed past seconds Salesforce has not stamped yet.
        server_horizon = self._settle_horizon()
        if server_horizon < horizon:
            return await self._seed_cursor_at(context, server_horizon)
        return cursor

    async def _seed_cursor_at(self, context: TriggerContext, horizon: datetime) -> Cursor:
        try:
            statement = render_soql(
                "SELECT Id, SystemModstamp FROM :object WHERE SystemModstamp <= :horizon "
                "ORDER BY SystemModstamp DESC, Id DESC LIMIT 1",
                {
                    "object": SOQLValue("identifier", self._configuration.s_object_type),
                    "horizon": SOQLValue("dateTime", format_rfc3339(horizon)),
                },
            )
        except ValueError as error:
            raise RecordPollError(f"Salesforce baseline query could not be built: {error}") from error
        page = await self._query(context, statement)
        position = RecordPollCursor(self._configuration.s_object_type, format_rfc3339(horizon))
        if page.records:
            modstamp = polled_record_modstamp(page.records[0])
            position.system_modstamp, position.record_id = format_rfc3339(modstamp), page.records[0].id
        return encode_record_poll_cursor(position)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[RecordModifiedEvent]]:
        """Read up to pollPageSize records ordered after cursor and stamped before the settle horizon.

        An undecodable cursor, or one for another object, is marked invalid so the kit reseeds.
        """
        try:
            position, after_instant = decode_record_poll_cursor(cursor.token)
        except ValueError as error:
            raise mark_cursor_invalid(error) from error
        if position.s_object_type != self._configuration.s_object_type:
            raise mark_cursor_invalid(ValueError("Salesforce poll cursor belongs to another object"))
        horizon = self._settle_horizon()
        if not after_instant.key() < (horizon, 0):
            return ChangePage((), cursor, False)
        try:
            statement = self._changes_statement(position, after_instant.moment, horizon)
        except ValueError as error:
            raise RecordPollError(f"Salesforce poll query could not be built: {error}") from error
        listed = await self._query(context, statement)
        page_size = self._client.poll_page_size
        if len(listed.records) > page_size:
            raise RecordPollError(
                f"Salesforce poll page has {len(listed.records)} records, "
                f"more than the SOQL LIMIT {page_size}"
            )
        # The response may show Salesforce's clock behind; later records wait for a poll past its horizon.
        server_horizon = self._settle_horizon()
        next_position = RecordPollCursor(position.s_object_type, position.system_modstamp, position.record_id)
        after = after_instant.key()
        changes: list[TriggerEvent[RecordModifiedEvent]] = []
        for record in listed.records:
            modstamp = polled_record_modstamp(record)
            if not _is_after_cursor((modstamp, 0), record.id, after, next_position.record_id):
                raise RecordPollError(
                    "Salesforce poll page is not ordered by SystemModstamp after the cursor"
                )
            if modstamp > server_horizon:
                return ChangePage(tuple(changes), encode_record_poll_cursor(next_position), False)
            changes.append(self._change_event(record, modstamp))
            after = (modstamp, 0)
            next_position.system_modstamp, next_position.record_id = format_rfc3339(modstamp), record.id
        # A full page, or a page Salesforce split, may hide more records; the next page continues from next.
        has_more_pages = len(listed.records) == page_size or not listed.is_done
        return ChangePage(tuple(changes), encode_record_poll_cursor(next_position), has_more_pages)

    def _change_event(self, record: Record, modstamp: datetime) -> TriggerEvent[RecordModifiedEvent]:
        s_object_type = self._configuration.s_object_type
        event = RecordModifiedEvent(
            s_object_type,
            record.id,
            change_kind=record_change_kind(record),
            system_modstamp=modstamp,
            record=record,
        )
        return TriggerEvent(record_modified_event_id(s_object_type, record.id, modstamp), modstamp, event)

    def _changes_statement(self, position: RecordPollCursor, after: datetime, horizon: datetime) -> str:
        """Select the records ordered after the cursor; Id breaks ties within one second."""
        bindings = {
            "object": SOQLValue("identifier", self._configuration.s_object_type),
            "after": SOQLValue("dateTime", format_rfc3339(after.replace(microsecond=0))),
            "horizon": SOQLValue("dateTime", format_rfc3339(horizon)),
        }
        condition = "SystemModstamp > :after"
        if position.record_id:
            condition = "(SystemModstamp > :after OR (SystemModstamp = :after AND Id > :recordId))"
            bindings["recordId"] = SOQLValue("string", position.record_id)
        # Field names are validated API names, so joining them cannot inject an expression.
        return render_soql(
            "SELECT "
            + ", ".join(selected_fields(self._configuration))
            + " FROM :object WHERE SystemModstamp <= :horizon AND "
            + condition
            + " ORDER BY SystemModstamp ASC, Id ASC LIMIT "
            + str(self._client.poll_page_size),
            bindings,
        )

    async def _query(self, context: TriggerContext, statement: str) -> QueryRecordsOutput:
        """Send one SOQL request; REQUEST_LIMIT_EXCEEDED or 429 pauses the connection through the kit."""
        await wait_for_poll_request(context)
        client = self._client
        try:
            credentials = await resolve_credential(client.credentials, self._call, client.refresh_driver)
        except ReauthorizationRequiredError:
            raise RecordPollError(
                "Salesforce authorization is revoked or expired; reauthorize the connection"
            ) from None
        except Exception:
            raise RecordPollError("Salesforce Trigger credentials are unavailable") from None
        if not client._valid_credentials(credentials):
            raise RecordPollError("Salesforce Trigger credentials are unavailable")
        query = {"q": statement}
        if len(go_query(query).encode()) > _MAX_ENCODED_QUERY_BYTES:
            raise RecordPollError("Salesforce poll query is too long; select fewer fields")
        response, outcome = await client.send(
            self._call,
            credentials,
            _OPERATION,
            "GET",
            client.data_path("query"),
            query=query,
            query_batch_size=max(_MINIMUM_QUERY_BATCH_SIZE, client.poll_page_size),
        )
        if response is not None:
            self._observe_server_clock(response.headers.get("Date", ""))
        if outcome is not None:
            if response is None:
                raise RecordPollError(f"Salesforce poll request failed: {outcome.failure.message}")
            failure = RecordPollError(f"Salesforce poll request: {outcome.failure.message}")
            if outcome.failure.kind is FailureKind.RATE_LIMIT:
                # The daily API allocation and concurrent request limit both report REQUEST_LIMIT_EXCEEDED.
                raise mark_rate_limited(failure, timedelta(seconds=outcome.retry_after)) from None
            raise failure
        assert response is not None
        try:
            return decode_query_page(client, response.status, response.body)
        except (ValueError, TypeError, UnicodeError):
            raise RecordPollError("Salesforce returned an invalid poll query page") from None

    def _settle_horizon(self) -> datetime:
        """Return the settle delay before the earlier of this host's and Salesforce's clocks."""
        moment = self._client.now().astimezone(UTC) + self._server_clock_offset - MODSTAMP_SETTLE_DELAY
        return moment.replace(microsecond=0)

    def _observe_server_clock(self, date_header: str) -> None:
        """Record how far Salesforce's clock, from a response Date header, is behind this host's."""
        try:
            server_time = parsedate_to_datetime(date_header)
        except (TypeError, ValueError, IndexError):
            return
        if server_time.tzinfo is None:
            server_time = server_time.replace(tzinfo=UTC)
        offset = server_time - self._client.now().astimezone(UTC)
        self._server_clock_offset = timedelta(0) if offset > -_SERVER_CLOCK_TOLERANCE else offset


def polled_record_modstamp(record: Record) -> datetime:
    """Require an ID and a whole-second SystemModstamp, which SOQL dateTime literals match."""
    text = string_field(record, "SystemModstamp")
    if not is_record_id(record.id) or text is None:
        raise RecordPollError("Salesforce poll record lacks Id or SystemModstamp")
    modstamp: Instant | None = parse_salesforce_datetime(text)
    if modstamp is None or not modstamp.is_whole_second():
        raise RecordPollError("Salesforce poll record SystemModstamp is not a whole-second dateTime")
    return modstamp.moment


def _is_after_cursor(
    modstamp: tuple[datetime, int], record_id: str, after: tuple[datetime, int], after_record_id: str
) -> bool:
    """Check the order SOQL promises without assuming how Salesforce collates Id values."""
    return modstamp > after or (
        modstamp == after and after_record_id != "" and not is_same_record_id(record_id, after_record_id)
    )
