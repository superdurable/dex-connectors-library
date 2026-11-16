# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one account's Twilio Messages list for the ``messageReceived`` poll Trigger.

Twilio has no change feed, so the cursor is a ``date_sent`` watermark (Unix seconds, scoped to the account
SID and receiving address) plus a keyed snapshot of the message SIDs delivered within a two-minute lookback.
The cursor token and snapshot keys match the Go connector byte for byte, so Go and Python replicas share a
binding's cursor.
"""

import hashlib
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Final
from urllib.parse import quote_plus, urlsplit

from dex_connectors.sdk import Call, ConnectionRef, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    ChangePage,
    Cursor,
    KeyedSnapshotOptions,
    apply_keyed_snapshot_changes,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import KeyedSnapshot, encode_go_json_value
from dex_connectors.sdk.providerhttp import parse_retry_after

from ._generated.definitions import MESSAGE_RECEIVED_TRIGGER_DEFINITION
from .client import Client, error_code, optional_string, reject_invalid_json_constant
from .models import Message, MessageReceivedEvent

INBOUND_MESSAGE_DIRECTION: Final = "inbound"
"""Twilio's direction value for a message the account received."""
MESSAGE_STATUS_RECEIVING: Final = "receiving"
"""Twilio received an inbound message and is still processing it, such as fetching MMS media."""
MESSAGE_STATUS_RECEIVED: Final = "received"
"""Twilio finished processing an inbound message."""

_TRIGGER_NAME: Final = MESSAGE_RECEIVED_TRIGGER_DEFINITION.trigger.trigger_name
# Keeps a page of full message resources well under the default response limit; Twilio allows 1 to 1000.
_INBOUND_LIST_PAGE_SIZE: Final = 200
# Bounds one listing at 2,000 messages; a larger window is split by DateSent.
_MAXIMUM_WINDOW_PAGES: Final = 10
# Halves a window down to one second from more than 500 years.
_MAXIMUM_WINDOW_NARROWINGS: Final = 34
# Relists recent seconds, so a message Twilio dates slightly late is still found.
_INBOUND_LOOKBACK_SECONDS: Final = 120
# Bounds how long a message still in receiving holds the watermark back.
_RECEIVING_HOLD_LIMIT: Final = timedelta(minutes=10)
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_ZERO_TIME: Final = datetime.min.replace(tzinfo=UTC)
_GO_INTEGER: Final = re.compile(r"[+-]?[0-9]+\Z", re.ASCII)
_INT64_RANGE: Final = range(-(1 << 63), 1 << 63)
_LONE_SURROGATE: Final = re.compile("[\ud800-\udfff]")
_RESOURCE_STRING_FIELDS: Final = (
    "sid",
    "account_sid",
    "messaging_service_sid",
    "from",
    "to",
    "status",
    "direction",
    "price",
    "price_unit",
    "date_created",
    "date_sent",
    "date_updated",
    "body",
)

INBOUND_SNAPSHOT_OPTIONS: Final = KeyedSnapshotOptions()
"""Bounds the keys of messages delivered within the lookback."""


def message_received_event_id(message_sid: str) -> str:
    """Return the Trigger event ID of one message; a SID is assigned once, so the ID never changes."""
    return "twilio-message-" + message_sid


def _unix_second(moment: datetime) -> int:
    return (moment - _UNIX_EPOCH) // timedelta(seconds=1)


def _format_rfc3339_second(second: int) -> str:
    return (_UNIX_EPOCH + timedelta(seconds=second)).strftime("%Y-%m-%dT%H:%M:%SZ")


def new_message_received_trigger_event(
    message: Message, body: str, now: datetime
) -> TriggerEvent[MessageReceivedEvent]:
    """Convert one inbound message; occurredAt is when Twilio received it, else created it, else now."""
    occurred_at = message.sent_at
    if occurred_at == _ZERO_TIME:
        occurred_at = message.created_at
    if occurred_at == _ZERO_TIME:
        occurred_at = now.astimezone(UTC)
    return TriggerEvent(
        message_received_event_id(message.sid), occurred_at, MessageReceivedEvent(message=message, body=body)
    )


@dataclass(frozen=True, slots=True)
class _ListedMessage:
    """One decoded list entry; sent_at_second is zero while Twilio reports no date_sent."""

    message: Message
    body: str
    sent_at_second: int


@dataclass(frozen=True, slots=True)
class _InboundMessageWindow:
    """A complete listing of [since, upper_bound]; a zero upper_bound is unbounded."""

    messages: list[_ListedMessage]
    upper_bound: int


@dataclass(frozen=True, slots=True)
class _InboundSnapshotKey:
    """Records one delivered message as ``<date_sent Unix second>-<SID>``, so pruning needs no values."""

    key: str

    def snapshot_key(self) -> str:
        """Return the dated message key."""
        return self.key

    def snapshot_hash(self) -> str:
        """Return a hash of the key; a delivered message never changes its entry."""
        return hashlib.sha256(self.key.encode()).hexdigest()[:16]

    def snapshot_value(self) -> bytes | None:
        """Return None because entries keep no values."""
        return None


def _inbound_snapshot_key_for(listed: _ListedMessage) -> str:
    return f"{listed.sent_at_second:011d}-{listed.message.sid}"


def _inbound_snapshot_key_second(key: str) -> int | None:
    """Parse the key's second prefix as Go's strconv.ParseInt does, else None."""
    prefix, separator, _ = key.partition("-")
    if not separator or not _GO_INTEGER.fullmatch(prefix):
        return None
    second = int(prefix)
    return second if second in _INT64_RANGE else None


def _latest_sent_at_second(messages: list[_ListedMessage]) -> int:
    return max((listed.sent_at_second for listed in messages), default=0)


class InboundMessageFetcher:
    """Reads one account's Messages list for the poll Trigger kit, at parity with the Go fetcher."""

    def __init__(self, client: Client, connection: ConnectionRef, to: str) -> None:
        """Bind the client, the connection whose credential each request uses, and the receiving address."""
        self._client = client
        self._to = to
        self._call: Call = new_trigger_credential_call(
            connection, MESSAGE_RECEIVED_TRIGGER_DEFINITION.trigger
        )

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """List the lookback before now and record every inbound message in it as already delivered."""
        now = _unix_second(self._client.now())
        since = now - _INBOUND_LOOKBACK_SECONDS
        window = await self._list_window(context, since, 0)
        if window.upper_bound != 0:
            raise RuntimeError(
                f"Twilio listed more than {_INBOUND_LIST_PAGE_SIZE * _MAXIMUM_WINDOW_PAGES} messages "
                "within the baseline lookback"
            )
        watermark = max(now, _latest_sent_at_second(window.messages))
        delivered = [
            _InboundSnapshotKey(_inbound_snapshot_key_for(listed))
            for listed in window.messages
            if self._is_received_here(listed) and listed.sent_at_second >= since
        ]
        return self._encode_cursor(watermark, None, delivered)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[MessageReceivedEvent]]:
        """Return, oldest first, up to 500 inbound messages dated from the watermark minus the lookback.

        A message still in receiving holds the watermark until received or ten minutes old. An
        undecodable cursor, or one for another account or receiving address, is marked invalid.
        """
        position = _decode_cursor_token(cursor.token)
        if position is None:
            raise mark_cursor_invalid(RuntimeError("Twilio messageReceived cursor is not decodable"))
        account_sid, to, position_watermark = position
        if account_sid != self._client.account_sid or to != self._to:
            raise mark_cursor_invalid(
                RuntimeError("Twilio messageReceived cursor belongs to another account or receiving address")
            )
        since = position_watermark - _INBOUND_LOOKBACK_SECONDS
        # A narrowed window must still pass the watermark, or the poll would make no progress.
        window = await self._list_window(context, since, position_watermark + 1)
        now = self._client.now()
        delivered = {entry.key for entry in cursor.snapshot.entries} if cursor.snapshot is not None else set()
        hold_bound = 0
        pending: list[_ListedMessage] = []
        for listed in window.messages:
            if not self._is_received_here(listed):
                continue
            if listed.sent_at_second == 0:
                hold_bound = _lower_hold_bound(hold_bound, position_watermark)
            elif listed.sent_at_second < since or _inbound_snapshot_key_for(listed) in delivered:
                continue
            elif (
                listed.message.status == MESSAGE_STATUS_RECEIVING
                and now - listed.message.sent_at < _RECEIVING_HOLD_LIMIT
            ):
                hold_bound = _lower_hold_bound(hold_bound, listed.sent_at_second)
            else:
                pending.append(listed)
        pending.sort(key=lambda listed: (listed.sent_at_second, listed.message.sid.encode()))
        has_more_pages = False
        is_page_full = len(pending) > MAXIMUM_CHANGES_PER_PAGE
        if is_page_full:
            pending = pending[:MAXIMUM_CHANGES_PER_PAGE]
            watermark, has_more_pages = pending[-1].sent_at_second, True
        elif window.upper_bound != 0:
            watermark, has_more_pages = window.upper_bound, True
        else:
            watermark = _latest_sent_at_second(window.messages)
        if hold_bound != 0 and watermark > hold_bound:
            watermark = hold_bound
            # Relisting a held window returns the same messages; only a full change page is worth refetching.
            has_more_pages = is_page_full
        watermark = max(watermark, position_watermark)
        changes = [new_message_received_trigger_event(listed.message, listed.body, now) for listed in pending]
        upserted = [_InboundSnapshotKey(_inbound_snapshot_key_for(listed)) for listed in pending]
        return ChangePage(changes, self._encode_cursor(watermark, cursor.snapshot, upserted), has_more_pages)

    def _is_received_here(self, listed: _ListedMessage) -> bool:
        """Report an inbound message to the watched address, or to any address when none is set."""
        return listed.message.direction == INBOUND_MESSAGE_DIRECTION and (
            self._to == "" or listed.message.to == self._to
        )

    async def _list_window(
        self, context: TriggerContext, since: int, minimum_upper_bound: int
    ) -> _InboundMessageWindow:
        """List every message dated from since, halving the upper bound toward the floor when too many."""
        floor = max(minimum_upper_bound, since) if minimum_upper_bound != 0 else since
        span = max(_unix_second(self._client.now()) - floor, 0)
        upper_bound = 0
        for _ in range(_MAXIMUM_WINDOW_NARROWINGS + 1):
            messages, is_truncated = await self._list_messages(context, since, upper_bound)
            if not is_truncated:
                return _InboundMessageWindow(messages, upper_bound)
            if upper_bound == floor:
                break
            span //= 2
            upper_bound = floor + span
        raise RuntimeError(
            f"Twilio listed more than {_INBOUND_LIST_PAGE_SIZE * _MAXIMUM_WINDOW_PAGES} messages within "
            "2m0s of the watermark; the messageReceived Trigger cannot page through them"
        )

    async def _list_messages(
        self, context: TriggerContext, since: int, upper_bound: int
    ) -> tuple[list[_ListedMessage], bool]:
        """Follow next_page_uri for at most ten pages and report whether more remain.

        Twilio lists newest first by DateSent, so a page reaching a message dated before since ends the
        listing. DateSent bounds are inclusive and also applied locally, since Twilio documents them as dates.
        """
        query = {"PageSize": str(_INBOUND_LIST_PAGE_SIZE), "DateSent>": _format_rfc3339_second(since)}
        if upper_bound != 0:
            query["DateSent<"] = _format_rfc3339_second(upper_bound)
        if self._to:
            query["To"] = self._to
        target = self._client.messages_url + ".json?" + _encode_go_query(query)
        messages: list[_ListedMessage] = []
        for _ in range(_MAXIMUM_WINDOW_PAGES):
            listed, next_page_uri = await self._list_page(context, target)
            has_reached_since = False
            for entry in listed:
                if entry.sent_at_second != 0 and entry.sent_at_second < since:
                    has_reached_since = True
                    continue
                if entry.sent_at_second != 0 and upper_bound != 0 and entry.sent_at_second > upper_bound:
                    continue
                messages.append(entry)
            if not next_page_uri or has_reached_since:
                return messages, False
            target = self._resolve_next_page(next_page_uri)
        return messages, True

    def _resolve_next_page(self, next_page_uri: str) -> str:
        """Accept only a next page of this account's Messages list on the configured host."""
        base = urlsplit(self._client.messages_url + ".json")
        try:
            following = urlsplit(next_page_uri)
        except ValueError:
            following = None
        if (
            following is None
            or following.scheme
            or following.netloc
            or following.path != base.path
            or not following.query
        ):
            raise RuntimeError("Twilio Messages list returned an unexpected next_page_uri")
        return f"{base.scheme}://{base.netloc}{following.path}?{following.query}"

    async def _list_page(self, context: TriggerContext, target: str) -> tuple[list[_ListedMessage], str]:
        """Send one Messages list request; a 429 pauses the connection for Retry-After or backs off."""
        await wait_for_poll_request(context)
        authorization, failure, is_transient = await self._client.authorization(self._call, _TRIGGER_NAME)
        if authorization is None:
            if is_transient or failure is None:
                raise RuntimeError("Twilio messageReceived credentials are unavailable")
            raise RuntimeError(failure.message)
        exchange = await self._client.exchange(authorization, "GET", target)
        if exchange.error:
            context.cancellation.raise_if_cancelled()
            if exchange.error == "notBuilt":
                raise RuntimeError("Twilio request could not be built")
            raise RuntimeError("Twilio Messages list request failed before a response")
        status = exchange.status
        if status == 429:
            retry_after = parse_retry_after(exchange.headers.get("Retry-After", ""), self._client.now())
            raise mark_rate_limited(
                RuntimeError(f"Twilio Messages list returned HTTP 429 (error {error_code(exchange.body)})"),
                timedelta(seconds=retry_after),
            )
        if status in (401, 403):
            raise RuntimeError(
                f"Twilio rejected the Messages list credentials (HTTP {status}, error "
                f"{error_code(exchange.body)}); update the connection or grant the API key Messages read "
                "access"
            )
        if not 200 <= status < 300:
            raise RuntimeError(
                f"Twilio Messages list returned HTTP {status} (error {error_code(exchange.body)})"
            )
        if exchange.has_body_read_failed:
            raise RuntimeError("Twilio Messages list response was interrupted")
        if exchange.is_body_too_large:
            raise RuntimeError(
                "Twilio Messages list page exceeds maxResponseBytes; raise it on the connection"
            )
        return self._decode_page(exchange.body, authorization)

    def _decode_page(
        self, contents: bytes, authorization: tuple[str, str]
    ) -> tuple[list[_ListedMessage], str]:
        invalid_json = "Twilio Messages list page is not valid JSON"
        try:
            page = json.loads(
                contents.decode("utf-8", errors="replace"), parse_constant=reject_invalid_json_constant
            )
        except ValueError:
            raise RuntimeError(invalid_json) from None
        resources = page.get("messages") if isinstance(page, dict) else None
        next_page_uri = page.get("next_page_uri") if isinstance(page, dict) else None
        if not isinstance(resources, list) or not (next_page_uri is None or isinstance(next_page_uri, str)):
            raise RuntimeError(invalid_json)
        # Go's json.Unmarshal into the typed resource rejects the whole page for a mistyped string field.
        for resource in resources:
            if not isinstance(resource, dict) or any(
                resource.get(name) is not None and not isinstance(resource.get(name), str)
                for name in _RESOURCE_STRING_FIELDS
            ):
                raise RuntimeError(invalid_json)
        listed: list[_ListedMessage] = []
        for resource in resources:
            try:
                message = self._client.convert_message_resource(resource, authorization)
            except ValueError:
                raise RuntimeError("Twilio Messages list page has an invalid message") from None
            # Go decodes a lone surrogate escape as U+FFFD.
            body = _LONE_SURROGATE.sub("�", optional_string(resource.get("body")))
            # A body that quotes the credential is dropped rather than wedging the poll.
            if authorization[1] and authorization[1] in body:
                body = ""
            sent_at_second = _unix_second(message.sent_at) if message.sent_at != _ZERO_TIME else 0
            listed.append(_ListedMessage(message, body, sent_at_second))
        return listed, next_page_uri or ""

    def _encode_cursor(
        self, watermark: int, previous: KeyedSnapshot | None, upserted: list[_InboundSnapshotKey]
    ) -> Cursor:
        """Add upserted to the snapshot and drop keys dated before the new lookback."""
        cutoff = watermark - _INBOUND_LOOKBACK_SECONDS
        expired: list[str] = []
        if previous is not None:
            for entry in previous.entries:
                second = _inbound_snapshot_key_second(entry.key)
                if second is None or second < cutoff:
                    expired.append(entry.key)
        try:
            snapshot = apply_keyed_snapshot_changes(previous, upserted, expired, INBOUND_SNAPSHOT_OPTIONS)
        except ValueError as error:
            raise RuntimeError(f"Twilio messageReceived snapshot: {error}") from error
        token = '{"accountSid":' + encode_go_json_value(self._client.account_sid).decode()
        if self._to:
            token += ',"to":' + encode_go_json_value(self._to).decode()
        return Cursor(token + f',"watermark":{watermark}}}', snapshot)


def _lower_hold_bound(hold_bound: int, second: int) -> int:
    return second if hold_bound == 0 or second < hold_bound else hold_bound


def _encode_go_query(query: dict[str, str]) -> str:
    """Encode as Go's url.Values.Encode: keys sorted, QueryEscape for keys and values."""
    return "&".join(quote_plus(name) + "=" + quote_plus(query[name]) for name in sorted(query))


def _decode_cursor_token(token: str) -> tuple[str, str, int] | None:
    """Decode the cursor token as Go's json.Unmarshal into inboundMessageCursor does.

    Keys match case-insensitively and the last duplicate wins; null leaves a field unset. Returns None
    for a token Go rejects or whose watermark is not positive.
    """
    try:
        pairs = json.loads(token, object_pairs_hook=list, parse_constant=reject_invalid_json_constant)
    except ValueError:
        return None
    if pairs is None:
        return None
    if not isinstance(pairs, list) or not all(isinstance(pair, tuple) for pair in pairs):
        return None
    account_sid, to, watermark = "", "", 0
    for name, value in pairs:
        field_name = name.casefold()
        if value is None or field_name not in ("accountsid", "to", "watermark"):
            continue
        if field_name == "watermark":
            if isinstance(value, bool) or not isinstance(value, int) or value not in _INT64_RANGE:
                return None
            watermark = value
        elif not isinstance(value, str):
            return None
        elif field_name == "accountsid":
            account_sid = value
        else:
            to = value
    return (account_sid, to, watermark) if watermark > 0 else None
