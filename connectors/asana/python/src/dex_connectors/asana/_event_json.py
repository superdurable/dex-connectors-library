# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Strict JSON scanning that keeps each value's raw bytes, as Go's json.RawMessage does.

Event IDs hash `json.Compact` of Asana change values, which keeps string escapes and number literals
exactly as sent, so the scanner returns every value with its source bytes.
"""

import json
import re
from dataclasses import dataclass
from typing import Final

_WHITESPACE: Final = b" \t\n\r"
_NUMBER: Final = re.compile(rb"-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?")
_STRING_ESCAPE: Final = re.compile(rb'\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4})')


class InvalidEventJSONError(ValueError):
    """The bytes are not one strict JSON value, or a value has the wrong JSON type for its Go field."""


@dataclass(frozen=True, slots=True)
class RawJSONValue:
    """One JSON value with its exact source bytes."""

    raw: bytes
    """The value's bytes, including any insignificant whitespace inside containers."""
    kind: str
    """object, array, string, number, true, false, or null."""
    members: tuple[tuple[str, "RawJSONValue"], ...] = ()
    """An object's members in source order; duplicate keys are kept."""
    elements: tuple["RawJSONValue", ...] = ()
    """An array's elements."""
    text: str = ""
    """A string's decoded text, with invalid UTF-8 replaced as Go does."""

    def member(self, name: str) -> "RawJSONValue | None":
        """Return the last member whose key matches name, case-insensitively as Go's decoder does."""
        folded = name.lower()
        found: RawJSONValue | None = None
        for key, value in self.members:
            if key == name or key.lower() == folded:
                found = value
        return found

    def optional_object(self, name: str) -> "RawJSONValue | None":
        """Return a member decoded into a Go pointer to struct: None when absent or null."""
        value = self.member(name)
        if value is None or value.kind == "null":
            return None
        if value.kind != "object":
            raise InvalidEventJSONError(f"{name} must be an object")
        return value

    def string_member(self, name: str) -> str:
        """Return a member decoded into a Go string: empty when absent or null."""
        value = self.member(name)
        if value is None or value.kind == "null":
            return ""
        if value.kind != "string":
            raise InvalidEventJSONError(f"{name} must be a string")
        return value.text

    def boolean_member(self, name: str) -> bool:
        """Return a member decoded into a Go bool: false when absent or null."""
        value = self.member(name)
        if value is None or value.kind == "null":
            return False
        if value.kind not in ("true", "false"):
            raise InvalidEventJSONError(f"{name} must be a boolean")
        return value.kind == "true"

    def array_member(self, name: str) -> tuple["RawJSONValue", ...]:
        """Return a member decoded into a Go slice: empty when absent or null."""
        value = self.member(name)
        if value is None or value.kind == "null":
            return ()
        if value.kind != "array":
            raise InvalidEventJSONError(f"{name} must be an array")
        return value.elements


def parse_raw_json(raw: bytes) -> RawJSONValue:
    """Scan exactly one JSON value surrounded only by whitespace.

    Raises InvalidEventJSONError for any syntax Go's encoding/json rejects, such as NaN, a trailing
    comma, or trailing data.
    """
    try:
        value, end = _scan_value(raw, _skip_whitespace(raw, 0))
    except RecursionError as error:
        raise InvalidEventJSONError("JSON nests too deeply") from error
    if _skip_whitespace(raw, end) != len(raw):
        raise InvalidEventJSONError("JSON has trailing data")
    return value


def compact_raw_json(raw: bytes) -> bytes:
    """Remove insignificant whitespace and keep every other byte, as Go's json.Compact does."""
    compacted = bytearray()
    is_in_string = False
    index = 0
    while index < len(raw):
        byte = raw[index]
        if is_in_string:
            compacted.append(byte)
            if byte == 0x5C:
                compacted.append(raw[index + 1])
                index += 1
            elif byte == 0x22:
                is_in_string = False
        elif byte not in _WHITESPACE:
            compacted.append(byte)
            is_in_string = byte == 0x22
        index += 1
    return bytes(compacted)


def _skip_whitespace(raw: bytes, index: int) -> int:
    while index < len(raw) and raw[index] in _WHITESPACE:
        index += 1
    return index


def _scan_value(raw: bytes, start: int) -> tuple[RawJSONValue, int]:
    if start >= len(raw):
        raise InvalidEventJSONError("JSON ends before a value")
    lead = raw[start]
    if lead == 0x7B:
        return _scan_object(raw, start)
    if lead == 0x5B:
        return _scan_array(raw, start)
    if lead == 0x22:
        end, text = _scan_string(raw, start)
        return RawJSONValue(raw[start:end], "string", text=text), end
    for literal in (b"true", b"false", b"null"):
        if raw.startswith(literal, start):
            end = start + len(literal)
            return RawJSONValue(raw[start:end], literal.decode()), end
    number = _NUMBER.match(raw, start)
    if number is None:
        raise InvalidEventJSONError("JSON has an invalid value")
    return RawJSONValue(raw[start : number.end()], "number"), number.end()


def _scan_object(raw: bytes, start: int) -> tuple[RawJSONValue, int]:
    members: list[tuple[str, RawJSONValue]] = []
    index = _skip_whitespace(raw, start + 1)
    if index < len(raw) and raw[index] == 0x7D:
        return RawJSONValue(raw[start : index + 1], "object"), index + 1
    while True:
        if index >= len(raw) or raw[index] != 0x22:
            raise InvalidEventJSONError("JSON object key must be a string")
        index, key = _scan_string(raw, index)
        index = _skip_whitespace(raw, index)
        if index >= len(raw) or raw[index] != 0x3A:
            raise InvalidEventJSONError("JSON object key must be followed by a colon")
        value, index = _scan_value(raw, _skip_whitespace(raw, index + 1))
        members.append((key, value))
        index = _skip_whitespace(raw, index)
        if index < len(raw) and raw[index] == 0x2C:
            index = _skip_whitespace(raw, index + 1)
            continue
        if index < len(raw) and raw[index] == 0x7D:
            return RawJSONValue(raw[start : index + 1], "object", members=tuple(members)), index + 1
        raise InvalidEventJSONError("JSON object is not closed")


def _scan_array(raw: bytes, start: int) -> tuple[RawJSONValue, int]:
    elements: list[RawJSONValue] = []
    index = _skip_whitespace(raw, start + 1)
    if index < len(raw) and raw[index] == 0x5D:
        return RawJSONValue(raw[start : index + 1], "array"), index + 1
    while True:
        value, index = _scan_value(raw, index)
        elements.append(value)
        index = _skip_whitespace(raw, index)
        if index < len(raw) and raw[index] == 0x2C:
            index = _skip_whitespace(raw, index + 1)
            continue
        if index < len(raw) and raw[index] == 0x5D:
            return RawJSONValue(raw[start : index + 1], "array", elements=tuple(elements)), index + 1
        raise InvalidEventJSONError("JSON array is not closed")


def _scan_string(raw: bytes, start: int) -> tuple[int, str]:
    index = start + 1
    while index < len(raw):
        byte = raw[index]
        if byte == 0x22:
            return index + 1, _decode_string(raw[start : index + 1])
        if byte < 0x20:
            raise InvalidEventJSONError("JSON string has a control character")
        if byte == 0x5C:
            escape = _STRING_ESCAPE.match(raw, index)
            if escape is None:
                raise InvalidEventJSONError("JSON string has an invalid escape")
            index = escape.end()
            continue
        index += 1
    raise InvalidEventJSONError("JSON string is not closed")


def _decode_string(quoted: bytes) -> str:
    # Escapes are validated; json.loads decodes them, and invalid UTF-8 becomes U+FFFD first.
    decoded: str = json.loads(quoted.decode("utf-8", "replace"))
    return decoded.encode("utf-16", "surrogatepass").decode("utf-16", "replace")
