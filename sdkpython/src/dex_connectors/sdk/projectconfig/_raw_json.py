# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go encoding/json emulation for Trigger inbox bytes shared with Go and TypeScript replicas.

Every scanner, decoder, and encoder here is iterative, so Go's 10000-level nesting limit, not the
Python call stack, bounds it.
"""

import dataclasses
import json
import math
import re
from datetime import UTC, datetime
from decimal import Decimal
from typing import Any, cast

import dex

from ..timestamps import format_rfc3339_nano

_MAXIMUM_NESTING_DEPTH = 10_000
_DATACLASS_CODECS = dex.CodecRegistry()
_WHITESPACE = b" \t\n\r"
_QUOTE, _BACKSLASH, _COMMA, _COLON = 0x22, 0x5C, 0x2C, 0x3A
_OPEN_BRACE, _CLOSE_BRACE, _OPEN_BRACKET, _CLOSE_BRACKET = 0x7B, 0x7D, 0x5B, 0x5D
_SIMPLE_ESCAPES = {
    0x22: 0x22,
    0x5C: 0x5C,
    0x2F: 0x2F,
    0x62: 0x08,
    0x66: 0x0C,
    0x6E: 0x0A,
    0x72: 0x0D,
    0x74: 0x09,
}
_HEX_DIGITS = frozenset(b"0123456789abcdefABCDEF")
_PLAIN_STRING_RUN = re.compile(rb'[^"\\\x00-\x1f]*')
_PLAIN_COMPACT_RUN = re.compile(rb'[^"\\<>&\xe2]*')
_ASCII_STRING_RUN = re.compile(rb"[\x00-\x5b\x5d-\x7f]+")
_ASCII_RUN = re.compile(rb"[\x00-\x7f]+")
_HTML_ESCAPES = {"<": "\\u003c", ">": "\\u003e", "&": "\\u0026", " ": "\\u2028", " ": "\\u2029"}


class InvalidRawJSONError(ValueError):
    """Raised for bytes that are not one complete JSON value, or a value the Go encoder cannot write."""

    def __init__(self, message: str = "raw JSON is invalid") -> None:
        """Create the error; the default message is "raw JSON is invalid"."""
        super().__init__(message)


def _skip_whitespace(raw: bytes, index: int) -> int:
    length = len(raw)
    while index < length and raw[index] in _WHITESPACE:
        index += 1
    return index


def _byte_at(raw: bytes, index: int) -> int:
    return raw[index] if index < len(raw) else -1


def _hex_unit(raw: bytes, index: int) -> int:
    digits = raw[index : index + 4]
    if len(digits) != 4 or any(digit not in _HEX_DIGITS for digit in digits):
        return -1
    return int(digits, 16)


def _scan_string(raw: bytes, index: int) -> int:
    if _byte_at(raw, index) != _QUOTE:
        raise InvalidRawJSONError()
    index += 1
    length = len(raw)
    while index < length:
        index = _PLAIN_STRING_RUN.match(raw, index).end()  # type: ignore[union-attr]
        if index >= length:
            break
        byte = raw[index]
        if byte == _QUOTE:
            return index + 1
        if byte < 0x20:
            raise InvalidRawJSONError()
        if byte == _BACKSLASH:
            index += 1
            escape = _byte_at(raw, index)
            if escape == 0x75:
                if _hex_unit(raw, index + 1) < 0:
                    raise InvalidRawJSONError()
                index += 4
            elif escape not in _SIMPLE_ESCAPES:
                raise InvalidRawJSONError()
        index += 1
    raise InvalidRawJSONError()


def _scan_literal(raw: bytes, index: int, literal: bytes) -> int:
    if raw[index : index + len(literal)] != literal:
        raise InvalidRawJSONError()
    return index + len(literal)


def _is_digit(raw: bytes, index: int) -> bool:
    return 0x30 <= _byte_at(raw, index) <= 0x39


def _scan_number(raw: bytes, index: int) -> int:
    if _byte_at(raw, index) == 0x2D:
        index += 1
    if _byte_at(raw, index) == 0x30:
        index += 1
    elif _is_digit(raw, index):
        while _is_digit(raw, index):
            index += 1
    else:
        raise InvalidRawJSONError()
    if _byte_at(raw, index) == 0x2E:
        index += 1
        if not _is_digit(raw, index):
            raise InvalidRawJSONError()
        while _is_digit(raw, index):
            index += 1
    if _byte_at(raw, index) in (0x65, 0x45):
        index += 1
        if _byte_at(raw, index) in (0x2B, 0x2D):
            index += 1
        if not _is_digit(raw, index):
            raise InvalidRawJSONError()
        while _is_digit(raw, index):
            index += 1
    return index


def _expect_key(raw: bytes, index: int) -> int:
    index = _skip_whitespace(raw, _scan_string(raw, index))
    if _byte_at(raw, index) != _COLON:
        raise InvalidRawJSONError()
    return index + 1


def _scan_value(raw: bytes, start: int) -> int:
    """Validate one value starting at start and return the index after it."""
    containers: list[int] = []
    index = start
    while True:
        index = _skip_whitespace(raw, index)
        byte = _byte_at(raw, index)
        if byte in (_OPEN_BRACE, _OPEN_BRACKET):
            if len(containers) >= _MAXIMUM_NESTING_DEPTH:
                raise InvalidRawJSONError()
            containers.append(byte)
            index = _skip_whitespace(raw, index + 1)
            close = _CLOSE_BRACE if byte == _OPEN_BRACE else _CLOSE_BRACKET
            if _byte_at(raw, index) == close:
                containers.pop()
                index += 1
            elif byte == _OPEN_BRACE:
                index = _expect_key(raw, index)
                continue
            else:
                continue
        elif byte == _QUOTE:
            index = _scan_string(raw, index)
        elif byte == 0x74:
            index = _scan_literal(raw, index, b"true")
        elif byte == 0x66:
            index = _scan_literal(raw, index, b"false")
        elif byte == 0x6E:
            index = _scan_literal(raw, index, b"null")
        else:
            index = _scan_number(raw, index)
        # A complete value ends here; close finished containers, then continue with the next sibling.
        while True:
            if not containers:
                return index
            index = _skip_whitespace(raw, index)
            container = containers[-1]
            close = _CLOSE_BRACE if container == _OPEN_BRACE else _CLOSE_BRACKET
            if _byte_at(raw, index) == close:
                containers.pop()
                index += 1
                continue
            if _byte_at(raw, index) != _COMMA:
                raise InvalidRawJSONError()
            index = _skip_whitespace(raw, index + 1)
            if container == _OPEN_BRACE:
                index = _expect_key(raw, index)
            break


def _whole_value_bounds(raw: bytes) -> tuple[int, int]:
    start = _skip_whitespace(raw, 0)
    end = _scan_value(raw, start)
    if _skip_whitespace(raw, end) != len(raw):
        raise InvalidRawJSONError()
    return start, end


def _compact_go_raw_json(raw: bytes) -> bytes:
    """Validate one value and compact it as Go writes a json.RawMessage."""
    raw = bytes(raw)
    start, end = _whole_value_bounds(raw)
    output = bytearray()
    is_in_string = False
    index = start
    while index < end:
        byte = raw[index]
        if not is_in_string:
            if byte not in _WHITESPACE:
                output.append(byte)
                is_in_string = byte == _QUOTE
            index += 1
            continue
        run_end = _PLAIN_COMPACT_RUN.match(raw, index).end()  # type: ignore[union-attr]
        if run_end > index:
            output += raw[index:run_end]
            index = run_end
            continue
        if byte == _BACKSLASH:
            output += raw[index : index + 2]
            index += 2
            continue
        if byte == _QUOTE:
            is_in_string = False
        if byte in (0x3C, 0x3E, 0x26):
            output += b"\\u00%x" % byte
            index += 1
            continue
        if (
            byte == 0xE2
            and raw[index + 1 : index + 2] == b"\x80"
            and raw[index + 2 : index + 3] in (b"\xa8", b"\xa9")
        ):
            output += b"\\u2028" if raw[index + 2] == 0xA8 else b"\\u2029"
            index += 3
            continue
        output.append(byte)
        index += 1
    return bytes(output)


def _raw_json_kind(raw: bytes) -> str:
    """Return object, array, string, number, true, false, or null for one complete value."""
    start, _ = _whole_value_bounds(raw)
    first = raw[start]
    return {
        _OPEN_BRACE: "object",
        _OPEN_BRACKET: "array",
        _QUOTE: "string",
        0x74: "true",
        0x66: "false",
        0x6E: "null",
    }.get(first, "number")


def _finish_container(raw: bytes, close_index: int) -> None:
    if _skip_whitespace(raw, close_index + 1) != len(raw):
        raise InvalidRawJSONError()


def _raw_json_object_members(raw: bytes) -> list[tuple[str, bytes]]:
    """Split one object into (decoded key, raw value) pairs in document order, duplicates kept."""
    index = _skip_whitespace(raw, 0)
    if _byte_at(raw, index) != _OPEN_BRACE:
        raise InvalidRawJSONError()
    members: list[tuple[str, bytes]] = []
    index = _skip_whitespace(raw, index + 1)
    if _byte_at(raw, index) == _CLOSE_BRACE:
        _finish_container(raw, index)
        return members
    while True:
        key_end = _scan_string(raw, index)
        key = _decode_go_json_string(raw[index:key_end])
        index = _skip_whitespace(raw, key_end)
        if _byte_at(raw, index) != _COLON:
            raise InvalidRawJSONError()
        value_start = _skip_whitespace(raw, index + 1)
        value_end = _scan_value(raw, value_start)
        members.append((key, raw[value_start:value_end]))
        index = _skip_whitespace(raw, value_end)
        if _byte_at(raw, index) == _CLOSE_BRACE:
            _finish_container(raw, index)
            return members
        if _byte_at(raw, index) != _COMMA:
            raise InvalidRawJSONError()
        index = _skip_whitespace(raw, index + 1)


def _raw_json_array_elements(raw: bytes) -> list[bytes]:
    """Split one array into the raw bytes of its elements."""
    index = _skip_whitespace(raw, 0)
    if _byte_at(raw, index) != _OPEN_BRACKET:
        raise InvalidRawJSONError()
    elements: list[bytes] = []
    index = _skip_whitespace(raw, index + 1)
    if _byte_at(raw, index) == _CLOSE_BRACKET:
        _finish_container(raw, index)
        return elements
    while True:
        end = _scan_value(raw, index)
        elements.append(raw[index:end])
        index = _skip_whitespace(raw, end)
        if _byte_at(raw, index) == _CLOSE_BRACKET:
            _finish_container(raw, index)
            return elements
        if _byte_at(raw, index) != _COMMA:
            raise InvalidRawJSONError()
        index = _skip_whitespace(raw, index + 1)


def _continuation(raw: bytes, index: int, low: int = 0x80, high: int = 0xBF) -> int:
    byte = _byte_at(raw, index)
    return byte & 0x3F if low <= byte <= high else -1


def _decode_go_rune(raw: bytes, index: int) -> tuple[int, int]:
    """Decode one UTF-8 sequence as Go's utf8.DecodeRune: an invalid sequence is U+FFFD of one byte."""
    first = raw[index]
    if first < 0x80:
        return first, 1
    if 0xC2 <= first <= 0xDF:
        second = _continuation(raw, index + 1)
        return (0xFFFD, 1) if second < 0 else (((first & 0x1F) << 6) | second, 2)
    if 0xE0 <= first <= 0xEF:
        second = _continuation(
            raw, index + 1, 0xA0 if first == 0xE0 else 0x80, 0x9F if first == 0xED else 0xBF
        )
        third = -1 if second < 0 else _continuation(raw, index + 2)
        return (0xFFFD, 1) if third < 0 else (((first & 0x0F) << 12) | (second << 6) | third, 3)
    if 0xF0 <= first <= 0xF4:
        second = _continuation(
            raw, index + 1, 0x90 if first == 0xF0 else 0x80, 0x8F if first == 0xF4 else 0xBF
        )
        third = -1 if second < 0 else _continuation(raw, index + 2)
        fourth = -1 if third < 0 else _continuation(raw, index + 3)
        if fourth < 0:
            return 0xFFFD, 1
        return ((first & 0x07) << 18) | (second << 12) | (third << 6) | fourth, 4
    return 0xFFFD, 1


def _decode_go_json_string(raw: bytes) -> str:
    """Decode one JSON string, quotes included, as Go does.

    Invalid UTF-8 bytes and lone surrogate escapes become U+FFFD.
    """
    if _byte_at(raw, 0) != _QUOTE or _scan_string(raw, 0) != len(raw):
        raise InvalidRawJSONError()
    characters: list[str] = []
    index = 1
    end = len(raw) - 1
    while index < end:
        byte = raw[index]
        if byte == _BACKSLASH:
            escape = raw[index + 1]
            if escape != 0x75:
                characters.append(chr(_SIMPLE_ESCAPES[escape]))
                index += 2
                continue
            unit = _hex_unit(raw, index + 2)
            index += 6
            if 0xD800 <= unit <= 0xDFFF:
                is_escape_next = _byte_at(raw, index) == _BACKSLASH and _byte_at(raw, index + 1) == 0x75
                low = _hex_unit(raw, index + 2) if is_escape_next else -1
                if unit <= 0xDBFF and 0xDC00 <= low <= 0xDFFF:
                    unit = 0x10000 + ((unit - 0xD800) << 10) + (low - 0xDC00)
                    index += 6
                else:
                    unit = 0xFFFD
            characters.append(chr(unit))
            continue
        if byte < 0x80:
            run_end = min(_ASCII_STRING_RUN.match(raw, index).end(), end)  # type: ignore[union-attr]
            characters.append(raw[index:run_end].decode("ascii"))
            index = run_end
            continue
        code_point, size = _decode_go_rune(raw, index)
        characters.append(chr(code_point))
        index += size
    return "".join(characters)


def _go_text(raw: bytes) -> str:
    """Decode bytes as UTF-8 with each invalid byte replaced as Go replaces it."""
    characters: list[str] = []
    index = 0
    length = len(raw)
    while index < length:
        if raw[index] < 0x80:
            run_end = _ASCII_RUN.match(raw, index).end()  # type: ignore[union-attr]
            characters.append(raw[index:run_end].decode("ascii"))
            index = run_end
            continue
        code_point, size = _decode_go_rune(raw, index)
        characters.append(chr(code_point))
        index += size
    return "".join(characters)


def _parse_number(literal: bytes) -> int | float:
    text = literal.decode("ascii")
    number = float(text)
    if math.isinf(number):
        raise InvalidRawJSONError("JSON number is outside the double range")
    if any(character in text for character in ".eE"):
        return number
    return int(text)


def parse_trigger_payload_json(raw: bytes | str) -> Any:
    """Parse a Trigger payload's raw JSON as Go's encoding/json decodes it, keeping integers exact.

    Integer literals become `int` and every other number `float`, unlike Go and TypeScript, which yield
    float64 for both. A number beyond the double range, such as 1e400, is refused, as Go refuses it; a
    value too small for a double, such as 1e-400, becomes 0.0. Strings and keys decode with Go's rules:
    an unpaired surrogate escape and each byte of an invalid UTF-8 sequence become U+FFFD. Duplicate keys
    keep the last value. Raises InvalidRawJSONError for anything but one complete JSON value.
    """
    data = _well_formed(raw).encode("utf-8") if isinstance(raw, str) else bytes(raw)
    start, _ = _whole_value_bounds(data)
    # Each frame is a container plus its pending key (objects only).
    stack: list[tuple[dict[str, Any] | list[Any], str | None]] = []
    result: Any = None
    index = start
    while True:
        index = _skip_whitespace(data, index)
        byte = data[index]
        value: Any
        if byte in (_OPEN_BRACE, _OPEN_BRACKET):
            container: dict[str, Any] | list[Any] = {} if byte == _OPEN_BRACE else []
            index = _skip_whitespace(data, index + 1)
            close = _CLOSE_BRACE if byte == _OPEN_BRACE else _CLOSE_BRACKET
            if data[index] != close:
                key: str | None = None
                if byte == _OPEN_BRACE:
                    key_end = _scan_string(data, index)
                    key = _decode_go_json_string(data[index:key_end])
                    index = _skip_whitespace(data, key_end) + 1
                stack.append((container, key))
                continue
            index += 1
            value = container
        elif byte == _QUOTE:
            string_end = _scan_string(data, index)
            value = _decode_go_json_string(data[index:string_end])
            index = string_end
        elif byte == 0x74:
            value, index = True, index + 4
        elif byte == 0x66:
            value, index = False, index + 5
        elif byte == 0x6E:
            value, index = None, index + 4
        else:
            number_end = _scan_number(data, index)
            value = _parse_number(data[index:number_end])
            index = number_end
        while True:
            if not stack:
                result = value
                break
            parent, parent_key = stack[-1]
            if isinstance(parent, dict):
                parent[parent_key or ""] = value
            else:
                parent.append(value)
            index = _skip_whitespace(data, index)
            if data[index] == _COMMA:
                index = _skip_whitespace(data, index + 1)
                if isinstance(parent, dict):
                    key_end = _scan_string(data, index)
                    stack[-1] = (parent, _decode_go_json_string(data[index:key_end]))
                    index = _skip_whitespace(data, key_end) + 1
                break
            # The scanner validated the document, so this byte closes the parent.
            stack.pop()
            index += 1
            value = parent
        if not stack:
            return result


def _well_formed(text: str) -> str:
    """Replace each unpaired surrogate with U+FFFD, as JavaScript toWellFormed does."""
    if not any(0xD800 <= ord(character) <= 0xDFFF for character in text):
        return text
    return "".join("\ufffd" if 0xD800 <= ord(character) <= 0xDFFF else character for character in text)


def _encode_go_string(text: str) -> str:
    """Encode a string as Go does: unpaired surrogates as U+FFFD and HTML-safe escapes."""
    encoded = json.dumps(_well_formed(text), ensure_ascii=False)
    if any(character in encoded for character in _HTML_ESCAPES):
        encoded = "".join(_HTML_ESCAPES.get(character, character) for character in encoded)
    return encoded


def _format_go_float(value: float) -> str:
    if math.isnan(value) or math.isinf(value):
        raise InvalidRawJSONError("JSON cannot encode NaN or Infinity")
    if value == 0:
        return "-0" if math.copysign(1.0, value) < 0 else "0"
    sign, digit_tuple, exponent = Decimal(repr(value)).as_tuple()
    digits = "".join(str(digit) for digit in digit_tuple).lstrip("0")
    stripped = digits.rstrip("0")
    exponent = int(exponent) + len(digits) - len(stripped)
    digits = stripped
    # ES Number::toString: value = 0.digits x 10^point.
    point = len(digits) + exponent
    count = len(digits)
    prefix = "-" if sign else ""
    if count <= point <= 21:
        return prefix + digits + "0" * (point - count)
    if 0 < point <= 21:
        return prefix + digits[:point] + "." + digits[point:]
    if -6 < point <= 0:
        return prefix + "0." + "0" * (-point) + digits
    scientific = point - 1
    exponent_text = ("+" if scientific >= 0 else "-") + str(abs(scientific))
    mantissa = digits if count == 1 else digits[0] + "." + digits[1:]
    return prefix + mantissa + "e" + exponent_text


def _format_go_time(value: datetime) -> str:
    if value.tzinfo is None or value.utcoffset() is None:
        raise InvalidRawJSONError("JSON cannot encode a time without a timezone offset")
    return format_rfc3339_nano(value.astimezone(UTC))


class _EncodingFrame:
    __slots__ = ("container", "index", "is_object", "items")

    def __init__(
        self, container: object, items: list[object], is_object: bool
    ) -> None:
        self.container = container
        self.items: list[object] = items
        self.is_object = is_object
        self.index = 0


def _dataclass_json_value(value: object) -> Any:
    try:
        encoded = _DATACLASS_CODECS.resolve(type(value)).encode(value)
    except (TypeError, ValueError, dex.ValueMappingError) as error:
        raise InvalidRawJSONError(f"JSON cannot encode a {type(value).__name__}") from error
    if not isinstance(encoded.data, str):
        raise InvalidRawJSONError(f"JSON cannot encode a {type(value).__name__}")
    return json.loads(encoded.data)


def encode_go_json_value(value: object) -> bytes:
    """Encode a value as Go's encoding/json encodes the equivalent map, slice, string, bool, or number.

    The output is compact and HTML-safe, and object keys are sorted by their UTF-8 bytes as Go sorts map
    keys. `dict` keys must be `str`; `list` and `tuple` become arrays; `bool`, `int` (exact), `float`
    (Go's shortest round-trip format), and None map directly; an aware `datetime` encodes as RFC 3339 in
    UTC; an object with a `to_wire()` method is encoded through it once; and a dataclass instance, such
    as a generated connector model, is encoded through the Dex codec, which honors its `dex_json_*`
    field metadata. NaN, Infinity, naive times, cycles, other types, values the Dex codec refuses, and
    nesting deeper than 10000 levels raise InvalidRawJSONError.
    """
    parts: list[str] = []
    frames: list[_EncodingFrame] = []
    ancestors: set[int] = set()

    def write(current: object) -> None:
        to_wire = getattr(current, "to_wire", None)
        if callable(to_wire) and not isinstance(current, type):
            resolved = to_wire()
            if resolved is current or id(resolved) in ancestors:
                raise InvalidRawJSONError("JSON cannot encode a cycle")
            current = resolved
        elif dataclasses.is_dataclass(current) and not isinstance(current, type):
            current = _dataclass_json_value(current)
        if current is None:
            parts.append("null")
        elif isinstance(current, bool):
            parts.append("true" if current else "false")
        elif isinstance(current, int):
            parts.append(str(int(current)))
        elif isinstance(current, float):
            parts.append(_format_go_float(current))
        elif isinstance(current, str):
            parts.append(_encode_go_string(current))
        elif isinstance(current, datetime):
            parts.append(_encode_go_string(_format_go_time(current)))
        elif isinstance(current, dict | list | tuple):
            if id(current) in ancestors:
                raise InvalidRawJSONError("JSON cannot encode a cycle")
            if len(frames) >= _MAXIMUM_NESTING_DEPTH:
                raise InvalidRawJSONError("value is nested too deeply")
            ancestors.add(id(current))
            if isinstance(current, dict):
                if any(not isinstance(key, str) for key in current):
                    raise InvalidRawJSONError("JSON object keys must be strings")
                members = sorted(current.items(), key=lambda item: _well_formed(item[0]).encode("utf-8"))
                parts.append("{")
                frames.append(_EncodingFrame(current, list(members), True))
            else:
                parts.append("[")
                frames.append(_EncodingFrame(current, list(current), False))
        else:
            raise InvalidRawJSONError(f"JSON cannot encode a {type(current).__name__}")

    write(value)
    while frames:
        frame = frames[-1]
        if frame.index >= len(frame.items):
            parts.append("}" if frame.is_object else "]")
            ancestors.discard(id(frame.container))
            frames.pop()
            continue
        item = frame.items[frame.index]
        if frame.index > 0:
            parts.append(",")
        frame.index += 1
        if frame.is_object:
            key, member = cast(tuple[str, object], item)
            parts.append(_encode_go_string(key))
            parts.append(":")
            write(member)
        else:
            write(item)
    return "".join(parts).encode("utf-8")


_INBOX_SCHEMA_VERSION = "connectors.dex.dev/project-trigger-inbox/v1alpha1"


def _is_go_field_key(key: str, field_name: str) -> bool:
    """Match a key to a Go struct field as encoding/json does, with Unicode simple folding."""
    return key == field_name or key.lower().replace("ſ", "s") == field_name.lower()


def _decode_go_string_field(raw: bytes, current: str) -> str:
    kind = _raw_json_kind(raw)
    if kind == "null":
        return current
    if kind != "string":
        raise InvalidRawJSONError()
    return _decode_go_json_string(raw[_skip_whitespace(raw, 0) :])


def _decode_pending_event(raw: bytes) -> tuple[str, bytes]:
    kind = _raw_json_kind(raw)
    if kind == "null":
        return "", b"null"
    if kind != "object":
        raise InvalidRawJSONError()
    event_id = ""
    event = b"null"
    for key, value in _raw_json_object_members(raw):
        if _is_go_field_key(key, "id"):
            event_id = _decode_go_string_field(value, event_id)
        elif _is_go_field_key(key, "event"):
            event = value
        else:
            raise InvalidRawJSONError()
    return event_id, event


def _decode_inbox_document(contents: bytes) -> list[tuple[str, bytes]]:
    """Decode an inbox document strictly, as Go's DisallowUnknownFields does; events keep raw bytes."""
    try:
        contents = bytes(contents)
        if _raw_json_kind(contents) != "object":
            raise InvalidRawJSONError()
        schema_version = ""
        events: list[tuple[str, bytes]] = []
        for key, value in _raw_json_object_members(contents):
            if _is_go_field_key(key, "schemaVersion"):
                schema_version = _decode_go_string_field(value, schema_version)
                continue
            if not _is_go_field_key(key, "events"):
                raise InvalidRawJSONError()
            kind = _raw_json_kind(value)
            if kind == "null":
                events = []
                continue
            if kind != "array":
                raise InvalidRawJSONError()
            events = [_decode_pending_event(element) for element in _raw_json_array_elements(value)]
        if schema_version != _INBOX_SCHEMA_VERSION:
            raise InvalidRawJSONError()
        return events
    except (InvalidRawJSONError, IndexError, KeyError, ValueError):
        raise ValueError("trigger inbox document is invalid") from None


def _encode_inbox_document(events: list[tuple[str, bytes]]) -> bytes:
    """Encode the document as Go marshals it: field order, compact, HTML-safe, raw events compacted."""
    parts = [f'{{"schemaVersion":{_encode_go_string(_INBOX_SCHEMA_VERSION)},"events":['.encode()]
    for index, (event_id, event) in enumerate(events):
        parts.append(f'{"," if index else ""}{{"id":{_encode_go_string(event_id)},"event":'.encode())
        parts.append(_compact_go_raw_json(event))
        parts.append(b"}")
    parts.append(b"]}")
    return b"".join(parts)
