# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The itemChanged polled items, snapshot entries, and event identities, byte-compatible with Go.

Go hashes `json.Marshal(Item)` of each polled item, so this module decodes a monday.com item resource as
the Go connector does and encodes it as Go's encoding/json would: HTML-escaped strings, RFC 3339 UTC
times with nanoseconds, omitted empty fields, and each raw column value compacted from the provider's
JSON text.
"""

import hashlib
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from enum import StrEnum
from typing import Any, Final
from urllib.parse import urlsplit

from dex_connectors.sdk import TriggerEvent

from .item import COLUMN_ID, COLUMN_TYPE, NUMERIC_ID
from .models import Item, ItemChangedEvent, ItemColumnValue, ItemGroup

MAX_COLUMN_TEXT_BYTES: Final = 4096
MAX_COLUMN_VALUE_BYTES: Final = 16384
NANOSECONDS_PER_SECOND: Final = 1_000_000_000
GO_ZERO_TIME_NANOSECONDS: Final = -62_135_596_800 * NANOSECONDS_PER_SECOND
"""Go's zero time.Time, 0001-01-01T00:00:00Z, in nanoseconds since the Unix epoch."""

_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_LONE_SURROGATE: Final = re.compile("[\ud800-\udfff]")
_GO_HTML_ESCAPES: Final = (
    ("<", "\\u003c"),
    (">", "\\u003e"),
    ("&", "\\u0026"),
    ("\u2028", "\\u2028"),
    ("\u2029", "\\u2029"),
)
# unicode.IsSpace, which Go's bytes.TrimSpace uses.
_GO_SPACE: Final = (
    "\t\n\v\f\r \x85\xa0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a"
    "\u2028\u2029\u202f\u205f\u3000"
)
_FRACTION: Final = r"(?:[.,]([0-9]+))?"
# The four layouts of Go's mondayTimeLayouts, in order.
_PROVIDER_TIME_LAYOUTS: Final = (
    re.compile(
        r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})"
        + _FRACTION
        + r"(Z|([+-])([0-9]{2}):([0-9]{2}))\Z"
    ),
    re.compile(
        r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})"
        + _FRACTION
        + r"(Z|([+-])([0-9]{2})([0-9]{2}))\Z"
    ),
    re.compile(
        r"([0-9]{4})-([0-9]{2})-([0-9]{2}) ([0-9]{2}):([0-9]{2}):([0-9]{2})"
        + _FRACTION
        + r" ([A-Z]{3}|[A-Z]{3}T|[A-Z]{4}T|WITA|ChST|MeST)\Z"
    ),
    re.compile(r"([0-9]{4})-([0-9]{2})-([0-9]{2}) ([0-9]{2}):([0-9]{2}):([0-9]{2})" + _FRACTION + r"\Z"),
)
_STRICT_RFC3339: Final = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:\.([0-9]+))?"
    r"(Z|([+-])([0-9]{2}):([0-9]{2}))\Z"
)


class ItemChangeKind(StrEnum):
    """The derived kind of one itemChanged delivery."""

    CREATED = "created"
    """An item the Trigger has not delivered before whose creation time falls after the previous poll's
    position, minus the lookback window. An item created and then edited between two polls is still
    created."""
    UPDATED = "updated"
    """Any other change monday.com reports by moving the item's last-updated time, such as a column value,
    name, or group change."""


def go_json_string(value: str) -> str:
    """Encode a string as Go's encoding/json does, replacing lone surrogates and escaping HTML."""
    serialized = json.dumps(_LONE_SURROGATE.sub("\ufffd", value), ensure_ascii=False)
    for character, escape in _GO_HTML_ESCAPES:
        serialized = serialized.replace(character, escape)
    return serialized


def go_trim_space(value: str) -> str:
    """Trim leading and trailing Unicode white space as Go's bytes.TrimSpace does."""
    return value.strip(_GO_SPACE)


def go_compact_json(raw: str) -> str:
    """Compact valid JSON text and escape HTML characters, as Go marshals a json.RawMessage."""
    compacted: list[str] = []
    is_in_string = is_escaped = False
    for character in raw:
        if is_in_string:
            if is_escaped:
                is_escaped = False
            elif character == "\\":
                is_escaped = True
            elif character == '"':
                is_in_string = False
        elif character == '"':
            is_in_string = True
        elif character in " \t\n\r":
            continue
        compacted.append(character)
    serialized = "".join(compacted)
    for character, escape in _GO_HTML_ESCAPES:
        serialized = serialized.replace(character, escape)
    return serialized


def _instant_nanoseconds(match: re.Match[str], offset_seconds: int) -> int | None:
    year, month, day, hour, minute, second = (int(part) for part in match.groups()[:6])
    fraction = match.group(7) or ""
    try:
        local = datetime(year, month, day, hour, minute, second, tzinfo=UTC)
    except ValueError:
        return None
    seconds = (local - _UNIX_EPOCH) // timedelta(seconds=1) - offset_seconds
    return seconds * NANOSECONDS_PER_SECOND + int(fraction[:9].ljust(9, "0"))


def parse_provider_time(value: object) -> int | None:
    """Return nanoseconds since the Unix epoch of a monday.com timestamp, or None as Go's zero time.

    It accepts the layouts of Go's mondayTimeLayouts; a zone abbreviation counts as UTC.
    """
    if not isinstance(value, str) or not value:
        return None
    for index, layout in enumerate(_PROVIDER_TIME_LAYOUTS):
        match = layout.fullmatch(value)
        if match is None:
            continue
        offset_seconds = 0
        if index < 2 and match.group(8) != "Z":
            sign, zone_hour, zone_minute = match.group(9), int(match.group(10)), int(match.group(11))
            if zone_hour > 23 or zone_minute > 59:
                continue
            offset_seconds = (zone_hour * 3600 + zone_minute * 60) * (1 if sign == "+" else -1)
        instant = _instant_nanoseconds(match, offset_seconds)
        if instant is not None:
            return instant
    return None


def parse_cursor_time(value: str) -> int | None:
    """Parse a cursor watermark as Go's time.Time UnmarshalJSON does; None when Go would refuse it."""
    match = _STRICT_RFC3339.fullmatch(value)
    if match is None:
        return None
    offset_seconds = 0
    if match.group(8) != "Z":
        zone_hour, zone_minute = int(match.group(10)), int(match.group(11))
        if zone_hour > 23 or zone_minute > 59:
            return None
        offset_seconds = (zone_hour * 3600 + zone_minute * 60) * (1 if match.group(9) == "+" else -1)
    return _instant_nanoseconds(match, offset_seconds)


def format_go_time(nanoseconds: int | None) -> str:
    """Format a UTC instant as Go's time.Time MarshalJSON does; None is Go's zero time."""
    if nanoseconds is None:
        nanoseconds = GO_ZERO_TIME_NANOSECONDS
    seconds, fraction = divmod(nanoseconds, NANOSECONDS_PER_SECOND)
    instant = _UNIX_EPOCH + timedelta(seconds=seconds)
    text = f"{instant.year:04d}-{instant:%m-%dT%H:%M:%S}"
    if fraction:
        text += "." + f"{fraction:09d}".rstrip("0")
    return text + "Z"


def instant_datetime(nanoseconds: int | None) -> datetime:
    """Return an instant as an aware UTC datetime truncated to microseconds; None is datetime.min."""
    if nanoseconds is None:
        return datetime.min.replace(tzinfo=UTC)
    return _UNIX_EPOCH + timedelta(microseconds=nanoseconds // 1000)


def _truncate_utf8(value: str, maximum_bytes: int) -> tuple[str, bool]:
    encoded = _LONE_SURROGATE.sub("\ufffd", value).encode()
    if len(encoded) <= maximum_bytes:
        return encoded.decode(), False
    cut = maximum_bytes
    while cut > 0 and encoded[cut] & 0xC0 == 0x80:
        cut -= 1
    return encoded[:cut].decode(), True


def _reject_constant(name: str) -> Any:
    raise ValueError(f"{name} is not JSON")


def _string_field(resource: dict[str, Any], key: str) -> str:
    value = resource.get(key)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError(f"item {key} is not a string")
    return value


@dataclass(frozen=True, slots=True)
class PolledItem:
    """One item a poll read, with the Go JSON its content hash, snapshot entry, and event ID use."""

    item: Item
    created_at: int | None
    """Nanoseconds since the Unix epoch, or None when monday.com's timestamp is absent or unreadable."""
    updated_at: int | None
    encoded: bytes
    """The item as Go's json.Marshal encodes it; its length bounds a batch."""

    def snapshot_key(self) -> str:
        """The decimal item ID."""
        return self.item.id

    def snapshot_hash(self) -> str:
        """The first 16 bytes of SHA-256 over the item's Go JSON."""
        return hashlib.sha256(self.encoded).digest()[:16].hex()

    def snapshot_value(self) -> bytes:
        """The item's last-updated time, so entries leave the snapshot behind the lookback window."""
        return ('{"updatedAt":"' + format_go_time(self.updated_at) + '"}').encode()


def decode_polled_item(resource: object) -> PolledItem:
    """Decode one items_page item as Go's decodeItemResource does; ValueError when Go would refuse it."""
    if not isinstance(resource, dict):
        raise ValueError("item is not an object")
    item_id, name = _string_field(resource, "id"), _string_field(resource, "name")
    if not NUMERIC_ID.fullmatch(item_id):
        raise ValueError("item ID is not a monday.com numeric ID")
    state = _string_field(resource, "state")
    if state not in ("", "active", "archived", "deleted"):
        raise ValueError("item state is not a monday.com state")
    url = _string_field(resource, "url")
    try:
        parsed_url = urlsplit(url)
        if parsed_url.scheme != "https" or not parsed_url.hostname or parsed_url.username is not None:
            url = ""
    except ValueError:
        url = ""
    board_id = ""
    board = resource.get("board")
    if board is not None:
        if not isinstance(board, dict) or not NUMERIC_ID.fullmatch(_string_field(board, "id")):
            raise ValueError("item board ID is not a monday.com numeric ID")
        board_id = board["id"]
    group = None
    group_resource = resource.get("group")
    if group_resource is not None:
        if not isinstance(group_resource, dict) or not COLUMN_ID.fullmatch(
            _string_field(group_resource, "id")
        ):
            raise ValueError("item group ID is not a monday.com group ID")
        group = ItemGroup(group_resource["id"], _string_field(group_resource, "title"))
    creator_id = _string_field(resource, "creator_id")
    if creator_id and not NUMERIC_ID.fullmatch(creator_id):
        raise ValueError("item creator ID is not a monday.com numeric ID")
    created_at = parse_provider_time(resource.get("created_at"))
    updated_at = parse_provider_time(resource.get("updated_at"))
    column_resources = resource.get("column_values")
    if column_resources is None:
        column_resources = []
    if not isinstance(column_resources, list):
        raise ValueError("item column values are not a list")
    columns = [_decode_column_value(column) for column in column_resources]
    fields = [f'"id":{go_json_string(item_id)}', f'"name":{go_json_string(name)}']
    if state:
        fields.append(f'"state":{go_json_string(state)}')
    if board_id:
        fields.append(f'"boardId":{go_json_string(board_id)}')
    if group is not None:
        group_json = f'"id":{go_json_string(group.id)}'
        if group.title:
            group_json += f',"title":{go_json_string(group.title)}'
        fields.append('"group":{' + group_json + "}")
    if creator_id:
        fields.append(f'"creatorId":{go_json_string(creator_id)}')
    if url:
        fields.append(f'"url":{go_json_string(url)}')
    if created_at is not None:
        fields.append(f'"createdAt":"{format_go_time(created_at)}"')
    if updated_at is not None:
        fields.append(f'"updatedAt":"{format_go_time(updated_at)}"')
    if columns:
        fields.append('"columnValues":[' + ",".join(encoded for _, encoded in columns) + "]")
    item = Item(
        item_id,
        _LONE_SURROGATE.sub("\ufffd", name),
        state or None,
        board_id,
        group,
        creator_id,
        url,
        instant_datetime(created_at),
        instant_datetime(updated_at),
        tuple(column for column, _ in columns),
    )
    return PolledItem(item, created_at, updated_at, ("{" + ",".join(fields) + "}").encode())


def _decode_column_value(resource: object) -> tuple[ItemColumnValue, str]:
    """Bound one column value as Go's decodeColumnValueResource does and return it with its Go JSON."""
    if not isinstance(resource, dict):
        raise ValueError("column is not an object")
    column_id, column_type = _string_field(resource, "id"), _string_field(resource, "type")
    if not COLUMN_ID.fullmatch(column_id):
        raise ValueError("column ID is not a monday.com column ID")
    if not COLUMN_TYPE.fullmatch(column_type):
        raise ValueError(f"column {column_id} type is not a monday.com column type")
    text, is_text_truncated = _truncate_utf8(_string_field(resource, "text"), MAX_COLUMN_TEXT_BYTES)
    raw_value = resource.get("value")
    raw_text: str | None
    if raw_value is None:
        raw_text = None
    elif isinstance(raw_value, str):
        raw_text = go_trim_space(_LONE_SURROGATE.sub("\ufffd", raw_value))
        if raw_text in ("", "null"):
            raw_text = None
    else:
        # A non-string value arrives already parsed; its re-encoding keeps key order.
        raw_text = json.dumps(raw_value, ensure_ascii=False, separators=(",", ":"))
    value: Any = None
    is_value_omitted = False
    encoded_value = ""
    if raw_text is not None:
        try:
            value = json.loads(raw_text, parse_constant=_reject_constant)
        except ValueError as error:
            raise ValueError(f"column {column_id} value is not JSON") from error
        if len(raw_text.encode()) > MAX_COLUMN_VALUE_BYTES:
            value, is_value_omitted = None, True
        else:
            encoded_value = go_compact_json(raw_text)
    fields = [f'"id":{go_json_string(column_id)}', f'"type":{go_json_string(column_type)}']
    if text:
        fields.append(f'"text":{go_json_string(text)}')
    if encoded_value:
        fields.append(f'"value":{encoded_value}')
    if is_text_truncated:
        fields.append('"textTruncated":true')
    if is_value_omitted:
        fields.append('"valueOmitted":true')
    column = ItemColumnValue(
        column_id,
        type=column_type,
        text=text,
        value=value,
        text_truncated=is_text_truncated,
        value_omitted=is_value_omitted,
    )
    return column, "{" + ",".join(fields) + "}"


def new_item_changed_trigger_event(
    board_id: str, polled: PolledItem, kind: ItemChangeKind
) -> TriggerEvent[ItemChangedEvent]:
    """Convert one changed item; its ID is a pure function of the board, item ID, and item content."""
    joined = f"{board_id}\x00{polled.item.id}\x00{polled.snapshot_hash()}"
    event_id = "monday-item-" + hashlib.sha256(joined.encode()).digest()[:16].hex()
    payload = ItemChangedEvent(board_id, change_kind=kind.value, item=polled.item)
    return TriggerEvent(event_id, instant_datetime(polled.updated_at), payload)
