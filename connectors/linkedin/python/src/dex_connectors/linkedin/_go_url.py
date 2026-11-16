# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go's net/url Parse and URL.String for the https URLs this connector keeps.

A picture URL is stored and hashed exactly as the Go connector stores it, so Go and Python replicas share
one profileChanged cursor. Strings below are byte strings, one character per byte (latin-1).
"""

import ipaddress
import re
from dataclasses import dataclass
from typing import Final, Literal

_Mode = Literal["path", "host", "fragment"]
_HEX: Final = frozenset("0123456789abcdefABCDEF")
_SCHEME: Final = re.compile(r"^([A-Za-z][A-Za-z0-9+.\-]*):")
_PORT: Final = re.compile(r"^:[0-9]*$")


@dataclass
class _ParsedURL:
    scheme: str
    host: str
    has_user: bool
    path: str
    raw_path: str
    force_query: bool
    raw_query: str
    fragment: str = ""
    raw_fragment: str = ""


def _should_escape(character: str, mode: _Mode) -> bool:
    if character.isascii() and character.isalnum():
        return False
    if mode == "host" and character in "!$&'()*+,;=:[]<>\"":
        return False
    if character in "-_.~":
        return False
    if character in "$&+,/:;=?@":
        if mode == "path":
            return character == "?"
        if mode == "fragment":
            return False
    return not (mode == "fragment" and character in "!()*")


def _unescape(text: str, mode: _Mode) -> str | None:
    """Go's unescape, or None for an invalid escape or host byte."""
    unescaped: list[str] = []
    index = 0
    while index < len(text):
        character = text[index]
        if character == "%":
            if index + 2 >= len(text) or text[index + 1] not in _HEX or text[index + 2] not in _HEX:
                return None
            value = int(text[index + 1 : index + 3], 16)
            # A host may escape only non-ASCII bytes, and %25 for an IPv6 zone.
            if mode == "host" and value < 0x80 and value != 0x25:
                return None
            unescaped.append(chr(value))
            index += 3
            continue
        if mode == "host" and ord(character) < 0x80 and _should_escape(character, mode):
            return None
        unescaped.append(character)
        index += 1
    return "".join(unescaped)


def _escape(text: str, mode: _Mode) -> str:
    return "".join(f"%{ord(c):02X}" if _should_escape(c, mode) else c for c in text)


def _is_valid_encoded(text: str, mode: _Mode) -> bool:
    return all(c in "!$&'()*+,;=:@[]%" or not _should_escape(c, mode) for c in text)


def _is_valid_optional_port(port: str) -> bool:
    return port == "" or _PORT.match(port) is not None


def _is_ipv6(text: str) -> bool:
    try:
        return isinstance(ipaddress.ip_address(text), ipaddress.IPv6Address)
    except ValueError:
        return False


def _parse_host(host: str) -> str | None:
    if "[" in host:
        opening, closing = host.rfind("["), host.rfind("]")
        if closing < 0 or not _is_valid_optional_port(host[closing + 1 :]):
            return None
        address, has_zone, zone = host[opening + 1 : closing].partition("%25")
        # Go checks the address part with netip.ParseAddr and keeps an escaped %25 zone identifier.
        hostname = _unescape(address, "host")
        unescaped_zone = _unescape(zone, "host") if has_zone else ""
        if hostname is None or unescaped_zone is None or "%" in hostname or not _is_ipv6(hostname):
            return None
        zone_suffix = f"%{unescaped_zone}" if has_zone else ""
        return f"[{hostname}{zone_suffix}]{host[closing + 1 :]}"
    colon = host.rfind(":")
    if colon >= 0 and not _is_valid_optional_port(host[colon:]):
        return None
    return _unescape(host, "host")


def _hostname(host: str) -> str:
    colon = host.rfind(":")
    name = host[:colon] if colon >= 0 and _is_valid_optional_port(host[colon:]) else host
    if name.startswith("[") and name.endswith("]"):
        name = name[1:-1]
    return name


def _parse_absolute_url(raw: str) -> _ParsedURL | None:
    """Go's url.Parse of an absolute URL with an authority; None for anything else or an error."""
    before_fragment, has_fragment, raw_fragment = raw.partition("#")
    if any(ord(c) < 0x20 or ord(c) == 0x7F for c in before_fragment):
        return None
    scheme_match = _SCHEME.match(before_fragment)
    if scheme_match is None:
        return None
    scheme = scheme_match.group(1)
    rest, raw_query, force_query = before_fragment[len(scheme) + 1 :], "", False
    if rest.endswith("?") and rest.find("?") == len(rest) - 1:
        force_query, rest = True, rest[:-1]
    elif "?" in rest:
        rest, _, raw_query = rest.partition("?")
    if not rest.startswith("//"):
        return None
    authority = rest[2:]
    slash = authority.find("/")
    rest = "" if slash < 0 else authority[slash:]
    if slash >= 0:
        authority = authority[:slash]
    at = authority.rfind("@")
    host = _parse_host(authority if at < 0 else authority[at + 1 :])
    path = _unescape(rest, "path")
    if host is None or path is None:
        return None
    parsed = _ParsedURL(
        scheme=scheme.lower(),
        host=host,
        has_user=at >= 0,
        path=path,
        raw_path="" if _escape(path, "path") == rest else rest,
        force_query=force_query,
        raw_query=raw_query,
    )
    if has_fragment and raw_fragment:
        fragment = _unescape(raw_fragment, "fragment")
        if fragment is None:
            return None
        parsed.fragment = fragment
        parsed.raw_fragment = "" if _escape(fragment, "fragment") == raw_fragment else raw_fragment
    return parsed


def _escaped_path(url: _ParsedURL) -> str:
    if (
        url.raw_path
        and _is_valid_encoded(url.raw_path, "path")
        and _unescape(url.raw_path, "path") == url.path
    ):
        return url.raw_path
    return "*" if url.path == "*" else _escape(url.path, "path")


def _escaped_fragment(url: _ParsedURL) -> str:
    if (
        url.raw_fragment
        and _is_valid_encoded(url.raw_fragment, "fragment")
        and _unescape(url.raw_fragment, "fragment") == url.fragment
    ):
        return url.raw_fragment
    return _escape(url.fragment, "fragment")


def _url_string(url: _ParsedURL) -> str:
    text = f"{url.scheme}:"
    if url.host or url.path:
        text += "//"
    text += _escape(url.host, "host")
    path = _escaped_path(url)
    if path and not path.startswith("/") and url.host:
        text += "/"
    text += path
    if url.force_query or url.raw_query:
        text += f"?{url.raw_query}"
    if url.fragment:
        text += f"#{_escaped_fragment(url)}"
    return text


def go_safe_url(bounded_value: str) -> str:
    """Return Go's safeURL of a bounded value: url.URL.String for https with a host and no user, else ""."""
    parsed = _parse_absolute_url(bounded_value.encode("utf-8", "surrogatepass").decode("latin-1"))
    if parsed is None or parsed.scheme != "https" or parsed.has_user or not _hostname(parsed.host):
        return ""
    return _url_string(parsed).encode("latin-1").decode("utf-8", "replace")
