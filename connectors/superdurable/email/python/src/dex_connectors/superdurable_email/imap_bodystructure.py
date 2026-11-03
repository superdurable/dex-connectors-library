# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Parse bounded IMAP BODYSTRUCTURE metadata without fetching attachment content."""

import re
from dataclasses import dataclass
from email.header import decode_header
from email.utils import decode_rfc2231
from urllib.parse import unquote_to_bytes


@dataclass(frozen=True, slots=True)
class _BodyPart:
    path: tuple[int, ...]
    media_type: str
    transfer_encoding: str
    encoded_size: int
    charset: str
    filename: str
    is_inline: bool
    is_attachment: bool


class _BodyStructureParser:
    def __init__(self, source: bytes) -> None:
        self.source = source
        self.position = 0

    def parse(self) -> object:
        self._skip_spaces()
        value = self._value()
        self._skip_spaces()
        if self.position != len(self.source):
            raise ValueError("trailing IMAP BODYSTRUCTURE data")
        return value

    def _skip_spaces(self) -> None:
        while self.position < len(self.source) and self.source[self.position] in b" \t\r\n":
            self.position += 1

    def _value(self, depth: int = 0) -> object:
        self._skip_spaces()
        if depth > 64:
            raise ValueError("IMAP BODYSTRUCTURE nesting is too deep")
        if self.position >= len(self.source):
            raise ValueError("incomplete IMAP BODYSTRUCTURE")
        character = self.source[self.position]
        if character == ord("("):
            self.position += 1
            values: list[object] = []
            while True:
                self._skip_spaces()
                if self.position >= len(self.source):
                    raise ValueError("unclosed IMAP BODYSTRUCTURE")
                if self.source[self.position] == ord(")"):
                    self.position += 1
                    return values
                values.append(self._value(depth + 1))
        if character == ord('"'):
            self.position += 1
            value = bytearray()
            while self.position < len(self.source):
                character = self.source[self.position]
                self.position += 1
                if character == ord('"'):
                    return value.decode("utf-8", errors="replace")
                if character == ord("\\"):
                    if self.position >= len(self.source):
                        raise ValueError("incomplete IMAP quoted string")
                    character = self.source[self.position]
                    self.position += 1
                value.append(character)
            raise ValueError("unclosed IMAP quoted string")
        start = self.position
        while self.position < len(self.source) and self.source[self.position] not in b" ()\t\r\n":
            self.position += 1
        if self.position == start:
            raise ValueError("invalid IMAP BODYSTRUCTURE atom")
        token = self.source[start : self.position].decode("ascii", errors="strict")
        if token.upper() == "NIL":
            return None
        if token.isdigit():
            return int(token)
        return token


def _extract_parenthesized(source: bytes, start: int) -> bytes:
    depth = 0
    quoted = False
    escaped = False
    for position in range(start, len(source)):
        character = source[position]
        if escaped:
            escaped = False
            continue
        if quoted and character == ord("\\"):
            escaped = True
            continue
        if character == ord('"'):
            quoted = not quoted
            continue
        if quoted:
            continue
        if character == ord("("):
            depth += 1
        elif character == ord(")"):
            depth -= 1
            if depth == 0:
                return source[start : position + 1]
    raise ValueError("incomplete IMAP BODYSTRUCTURE")


def _parameters(value: object) -> dict[str, str]:
    if not isinstance(value, list) or len(value) % 2:
        return {}
    result: dict[str, str] = {}
    for index in range(0, len(value), 2):
        key, item = value[index : index + 2]
        if isinstance(key, str) and isinstance(item, str):
            result[key.lower()] = item
    return result


def _decode_filename(parameters: dict[str, str]) -> str:
    encoded = parameters.get("filename*") or parameters.get("name*")
    if encoded:
        charset, _, value = decode_rfc2231(encoded)
        return unquote_to_bytes(value).decode(charset or "utf-8", errors="replace")
    for base in ("filename", "name"):
        segments: list[str] = []
        index = 0
        while (
            segment := parameters.get(f"{base}*{index}*") or parameters.get(f"{base}*{index}")
        ) is not None:
            segments.append(segment)
            index += 1
        if segments:
            joined = "".join(segments)
            charset, _, value = decode_rfc2231(joined)
            if charset:
                return unquote_to_bytes(value).decode(charset, errors="replace")
            return unquote_to_bytes(joined).decode("utf-8", errors="replace")
    direct = parameters.get("filename") or parameters.get("name")
    if direct:
        try:
            chunks = decode_header(direct)
            return "".join(
                chunk.decode(charset or "utf-8", errors="replace") if isinstance(chunk, bytes) else chunk
                for chunk, charset in chunks
            )
        except (LookupError, UnicodeError):
            return direct
    return ""


def _disposition(value: list[object]) -> tuple[str, dict[str, str]]:
    for item in value[7:]:
        if (
            isinstance(item, list)
            and item
            and isinstance(item[0], str)
            and item[0].upper() in ("ATTACHMENT", "INLINE")
        ):
            return item[0].lower(), _parameters(item[1] if len(item) > 1 else None)
    return "", {}


def _walk(structure: object, path: tuple[int, ...], result: list[_BodyPart]) -> None:
    if len(path) > 64 or len(result) > 1000:
        raise ValueError("IMAP BODYSTRUCTURE has too many parts")
    if not isinstance(structure, list) or not structure:
        raise ValueError("invalid IMAP BODYSTRUCTURE part")
    if isinstance(structure[0], list):
        index = 0
        while index < len(structure) and isinstance(structure[index], list):
            _walk(structure[index], (*path, index + 1), result)
            index += 1
        if index == len(structure) or not isinstance(structure[index], str):
            raise ValueError("multipart IMAP BODYSTRUCTURE lacks subtype")
        return
    if len(structure) < 7 or not isinstance(structure[0], str) or not isinstance(structure[1], str):
        raise ValueError("invalid single-part IMAP BODYSTRUCTURE")
    media_type = f"{structure[0]}/{structure[1]}".lower()
    if not isinstance(structure[5], str) or not isinstance(structure[6], int) or structure[6] < 0:
        raise ValueError("invalid IMAP BODYSTRUCTURE encoding or size")
    type_parameters = _parameters(structure[2])
    disposition, disposition_parameters = _disposition(structure)
    filename = _decode_filename(disposition_parameters) or _decode_filename(type_parameters)
    result.append(
        _BodyPart(
            path,
            media_type,
            structure[5].lower(),
            structure[6],
            type_parameters.get("charset", ""),
            filename,
            disposition == "inline",
            disposition == "attachment" or bool(filename) or media_type == "message/rfc822",
        )
    )


def parse_bodystructure(fetch_metadata: bytes) -> tuple[_BodyPart, ...]:
    """Decode a FETCH BODYSTRUCTURE expression into ordered MIME leaf metadata."""
    if re.search(rb"\bBODYSTRUCTURE\s+NIL\b", fetch_metadata, re.IGNORECASE):
        return ()
    match = re.search(rb"\bBODYSTRUCTURE\s*(\()", fetch_metadata, re.IGNORECASE)
    if match is None:
        raise ValueError("IMAP FETCH response has no BODYSTRUCTURE")
    expression = _extract_parenthesized(fetch_metadata, match.start(1))
    if len(expression) > 1 << 20:
        raise ValueError("IMAP BODYSTRUCTURE is too large")
    structure = _BodyStructureParser(expression).parse()
    parts: list[_BodyPart] = []
    _walk(structure, (), parts)
    return tuple(parts)
