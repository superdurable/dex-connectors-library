# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read the cursor of a Confluence ``_links.next`` link exactly as the Go connector's readNextCursor does.

Go parses the link with net/url's Parse and reads ``Query().Get("cursor")``: a link Parse rejects, a query
pair with a semicolon or a bad percent escape, and an empty first ``cursor`` value all differ from
urllib.parse, so this module mirrors Go's rules instead.
"""

import re
from typing import Final

_HEX: Final = frozenset(b"0123456789abcdefABCDEF")
_CURSOR: Final = re.compile(r"[\x21-\x7e]{1,4096}\Z", re.ASCII)
_USERINFO: Final = frozenset(
    b"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._:~!$&'()*+,;=%@"
)
_HOST: Final = frozenset(
    b"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~!$&'()*+,;=:[]<>\""
)
_SCHEME_LETTERS: Final = frozenset(b"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
_SCHEME_LATER: Final = frozenset(b"0123456789+-.")


class _GoURLError(ValueError):
    pass


def read_next_cursor(next_link: str) -> str:
    """Return the cursor of a next link, or "" for none; raises ValueError where Go's readNextCursor errs."""
    if not next_link:
        return ""
    cursor = _query_get(_parse_raw_query(next_link.encode("utf-8", "surrogatepass")), b"cursor")
    text = cursor.decode("latin-1")
    if not _CURSOR.match(text):
        raise ValueError("cursor is missing or invalid")
    return text


def _unescape(value: bytes, *, plus_is_space: bool = False, host: bool = False) -> bytes:
    """Mirror Go's url unescape: every percent sign starts two hex digits, else it is an error."""
    decoded = bytearray()
    index = 0
    while index < len(value):
        byte = value[index]
        if byte == 0x25:
            if index + 2 >= len(value) or value[index + 1] not in _HEX or value[index + 2] not in _HEX:
                raise _GoURLError("invalid URL escape")
            if host and int(value[index + 1 : index + 2], 16) < 8 and value[index : index + 3] != b"%25":
                raise _GoURLError("invalid URL escape in host")
            decoded.append(int(value[index + 1 : index + 3], 16))
            index += 3
            continue
        if host and byte < 0x80 and byte not in _HOST:
            raise _GoURLError("invalid character in host name")
        decoded.append(0x20 if plus_is_space and byte == 0x2B else byte)
        index += 1
    return bytes(decoded)


def _parse_raw_query(raw: bytes) -> bytes:
    """Validate raw as Go's url.Parse does and return its raw query."""
    link, _, fragment = raw.partition(b"#")
    if any(byte < 0x20 or byte == 0x7F for byte in link):
        raise _GoURLError("invalid control character in URL")
    scheme, rest = _scheme(link)
    if rest.endswith(b"?") and rest.count(b"?") == 1:
        rest, query = rest[:-1], b""
    else:
        rest, _, query = rest.partition(b"?")
    if not rest.startswith(b"/"):
        if scheme:
            _unescape(fragment)
            return query  # An opaque URL keeps its query; Go does not unescape the opaque part.
        if b":" in rest.partition(b"/")[0]:
            raise _GoURLError("first path segment in URL cannot contain colon")
    if rest.startswith(b"//") and (scheme or not rest.startswith(b"///")):
        authority, slash, path = rest[2:].partition(b"/")
        _parse_authority(authority)
        rest = slash + path
    _unescape(rest)
    _unescape(fragment)
    return query


def _scheme(link: bytes) -> tuple[bytes, bytes]:
    for index, byte in enumerate(link):
        if byte in _SCHEME_LETTERS:
            continue
        if byte in _SCHEME_LATER:
            if index == 0:
                return b"", link
            continue
        if byte == 0x3A:
            if index == 0:
                raise _GoURLError("missing protocol scheme")
            return link[:index], link[index + 1 :]
        return b"", link
    return b"", link


def _parse_authority(authority: bytes) -> None:
    userinfo, at, host = authority.rpartition(b"@")
    if at:
        if any(byte not in _USERINFO for byte in userinfo):
            raise _GoURLError("invalid userinfo")
        username, _, password = userinfo.partition(b":")
        _unescape(username)
        _unescape(password)
    if host.startswith(b"["):
        close = host.rfind(b"]")
        if close < 0:
            raise _GoURLError("missing ']' in host")
        _check_port(host[close + 1 :])
        _unescape(host[: close + 1], host=True)
        return
    colon = host.rfind(b":")
    if colon >= 0:
        _check_port(host[colon:])
    _unescape(host, host=True)


def _check_port(port: bytes) -> None:
    if port and (port[:1] != b":" or not port[1:].isdigit() and port[1:] != b""):
        raise _GoURLError("invalid port")


def _query_get(query: bytes, key: bytes) -> bytes:
    """Return the first value of key as Go's ParseQuery and Values.Get do, skipping malformed pairs."""
    for pair in query.split(b"&"):
        if not pair or b";" in pair:
            continue
        name, _, value = pair.partition(b"=")
        try:
            name, value = _unescape(name, plus_is_space=True), _unescape(value, plus_is_space=True)
        except _GoURLError:
            continue
        if name == key:
            return value
    return b""
