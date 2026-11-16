# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go text rules the connector reproduces byte for byte, so Go and Python compute one profile hash."""

import ipaddress
import re
from typing import Final

_ATOM: Final = "[!#-'*+\\-/-9=?A-Z^-~\u0080-\U0010ffff]+"
_DOT_ATOM: Final = re.compile(f"{_ATOM}(?:\\.{_ATOM})*")
_DOMAIN_LITERAL: Final = re.compile("\\[([!-Z^-~\u0080-\U0010ffff]*)\\]")


def go_decode_utf8(data: bytes) -> str:
    """Decode bytes as Go's encoding/json does inside strings: each invalid byte becomes one U+FFFD."""
    decoded: list[str] = []
    index = 0
    while index < len(data):
        size = _valid_rune_size(data, index)
        if size:
            decoded.append(data[index : index + size].decode())
            index += size
        else:
            decoded.append("\ufffd")
            index += 1
    return "".join(decoded)


def _valid_rune_size(data: bytes, index: int) -> int:
    first = data[index]
    if first < 0x80:
        return 1
    if 0xC2 <= first <= 0xDF:
        size, low, high = 2, 0x80, 0xBF
    elif 0xE0 <= first <= 0xEF:
        size, low, high = 3, 0xA0 if first == 0xE0 else 0x80, 0x9F if first == 0xED else 0xBF
    elif 0xF0 <= first <= 0xF4:
        size, low, high = 4, 0x90 if first == 0xF0 else 0x80, 0x8F if first == 0xF4 else 0xBF
    else:
        return 0
    following = data[index + 1 : index + size]
    if len(following) != size - 1 or not low <= following[0] <= high:
        return 0
    return size if all(0x80 <= byte <= 0xBF for byte in following[1:]) else 0


def go_to_lower(text: str) -> str:
    """Go's strings.ToLower: each character's simple lower-case mapping, so U+0130 becomes a plain i."""
    lowered: list[str] = []
    for character in text:
        mapped = character.lower()
        lowered.append(mapped if len(mapped) == 1 else "i" if character == "\u0130" else character)
    return "".join(lowered)


def go_canonical_email_address(trimmed: str) -> str:
    """Go's canonicalEmailAddress of a trimmed value: the lower-cased address, or "" when net/mail differs.

    net/mail must parse it as exactly one dot-atom addr-spec with a dot-atom domain or an IP domain literal,
    within 320 bytes. A quoted local part never matches, since net/mail returns it unquoted.
    """
    email = go_to_lower(trimmed)
    if not email or len(email.encode("utf-8", "surrogatepass")) > 320:
        return ""
    at = email.rfind("@")
    if at <= 0:
        return ""
    local, domain = email[:at], email[at + 1 :]
    if _DOT_ATOM.fullmatch(local) is None:
        return ""
    if _DOT_ATOM.fullmatch(domain) is not None:
        return email
    literal = _DOMAIN_LITERAL.fullmatch(domain)
    if literal is None or "%" in literal.group(1):
        return ""
    try:
        ipaddress.ip_address(literal.group(1))
    except ValueError:
        return ""
    return email
