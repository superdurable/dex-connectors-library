# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Sign and verify requests a Dex host relays to an application with its scope token.

The scheme matches the Go and TypeScript Connector SDKs: `Dex-Relay-Timestamp` carries the Unix second
of signing and `Dex-Relay-Signature` carries "v1=" and the hexadecimal HMAC-SHA256 of
"v1:<timestamp>:<raw body>" keyed with the scope token. A request is valid only when the signature
matches in constant time and the timestamp is within five minutes of receipt. These functions never
log, store, or return the token. Applications with a loaded project use
`LoadedProject.verify_relayed_request`, which reads the scope token and rereads it after a mismatch.

Type aliases:

| Name | Meaning |
|---|---|
| `RelayHeaders` | Request headers as a mapping or a sequence of name and value pairs. |
"""

import hashlib
import hmac
import re
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from enum import StrEnum
from typing import Final

RELAY_TIMESTAMP_HEADER: Final = "Dex-Relay-Timestamp"
"""Header carrying the Unix second at which the host signed a relayed request."""

RELAY_SIGNATURE_HEADER: Final = "Dex-Relay-Signature"
"""Header carrying "v1=" and the hexadecimal HMAC-SHA256 of "v1:<timestamp>:<body>"."""

RELAY_TIMESTAMP_TOLERANCE: Final = timedelta(minutes=5)
"""How far a relayed request's timestamp may be from the receiver's clock; the boundary is valid."""

RelayHeaders = Mapping[str, str] | Sequence[tuple[str, str]]
"""Request headers; names match case-insensitively and the first value of a name wins."""

_VERSION = "v1"
_TIMESTAMP = re.compile(r"-?[0-9]{1,15}", re.ASCII)
_SIGNATURE_HEX = re.compile(r"[0-9a-fA-F]{64}", re.ASCII)
_EPOCH = datetime(1970, 1, 1, tzinfo=UTC)
_MICROSECOND = timedelta(microseconds=1)


class RelaySignatureRefusal(StrEnum):
    """Why a relayed request is refused."""

    MISSING = "missing"
    """A relay header is absent or empty."""
    MALFORMED = "malformed"
    """A relay header does not have the v1 syntax."""
    STALE = "stale"
    """The timestamp is more than five minutes from receipt."""
    MISMATCH = "mismatch"
    """The signature does not match the token over the raw body."""


@dataclass(frozen=True, slots=True)
class ParsedRelaySignature:
    """The signed parts of one relayed request, parsed before the token is needed."""

    timestamp: str
    """The timestamp header text, used verbatim in the signed message."""
    signed_at: datetime
    """When the host signed the request, in UTC."""
    signature: bytes = field(repr=False)
    """The decoded HMAC-SHA256 digest."""


@dataclass(frozen=True, slots=True)
class RelaySignatureVerification:
    """The outcome of verifying one relayed request."""

    signed_at: datetime | None
    """When the host signed a valid request; None for a refusal."""
    refusal: RelaySignatureRefusal | None
    """Why the request is refused; None for a valid request."""

    @property
    def is_valid(self) -> bool:
        """Report whether the request is validly signed."""
        return self.refusal is None


def _microseconds(value: datetime) -> int:
    if value.tzinfo is None or value.utcoffset() is None:
        raise ValueError("relay signature time requires a timezone offset")
    return (value - _EPOCH) // _MICROSECOND


def _relay_header(headers: RelayHeaders, name: str) -> str:
    wanted = name.lower()
    items = headers.items() if isinstance(headers, Mapping) else headers
    for key, value in items:
        if key.lower() == wanted:
            return value
    return ""


def _relay_signature(token: str, timestamp: str, body: bytes) -> bytes:
    message = f"{_VERSION}:{timestamp}:".encode() + bytes(body)
    return hmac.new(token.encode(), message, hashlib.sha256).digest()


def sign_relayed_request(token: str, signed_at: datetime, body: bytes) -> str:
    """Return the `Dex-Relay-Signature` value for body signed at the Unix second of signed_at.

    Send the same Unix second in `Dex-Relay-Timestamp`. Raises ValueError for a naive datetime.
    """
    seconds = _microseconds(signed_at) // 1_000_000
    return f"{_VERSION}={_relay_signature(token, str(seconds), body).hex()}"


def parse_relay_signature(
    headers: RelayHeaders, received_at: datetime
) -> ParsedRelaySignature | RelaySignatureRefusal:
    """Parse the relay headers and check the timestamp window without the token.

    A receiver can refuse an unsigned, malformed, or stale request before reading any secret. Raises
    ValueError for a naive received_at.
    """
    received = _microseconds(received_at)
    timestamp = _relay_header(headers, RELAY_TIMESTAMP_HEADER)
    signature_header = _relay_header(headers, RELAY_SIGNATURE_HEADER)
    if not timestamp or not signature_header:
        return RelaySignatureRefusal.MISSING
    if not _TIMESTAMP.fullmatch(timestamp) or not signature_header.startswith(_VERSION + "="):
        return RelaySignatureRefusal.MALFORMED
    encoded = signature_header[len(_VERSION) + 1 :]
    if not _SIGNATURE_HEX.fullmatch(encoded):
        return RelaySignatureRefusal.MALFORMED
    seconds = int(timestamp)
    if abs(received - seconds * 1_000_000) > RELAY_TIMESTAMP_TOLERANCE // _MICROSECOND:
        return RelaySignatureRefusal.STALE
    return ParsedRelaySignature(timestamp, _EPOCH + timedelta(seconds=seconds), bytes.fromhex(encoded))


def matches_relay_signature(parsed: ParsedRelaySignature, token: str, body: bytes) -> bool:
    """Report whether a parsed signature matches token over the raw body, in constant time."""
    return hmac.compare_digest(parsed.signature, _relay_signature(token, parsed.timestamp, body))


def verify_relay_signature(
    token: str, headers: RelayHeaders, body: bytes, received_at: datetime
) -> RelaySignatureVerification:
    """Verify one relayed request's headers and raw body against token at received_at."""
    parsed = parse_relay_signature(headers, received_at)
    if isinstance(parsed, RelaySignatureRefusal):
        return RelaySignatureVerification(None, parsed)
    if not matches_relay_signature(parsed, token, body):
        return RelaySignatureVerification(None, RelaySignatureRefusal.MISMATCH)
    return RelaySignatureVerification(parsed.signed_at, None)


__all__ = [
    "RELAY_SIGNATURE_HEADER",
    "RELAY_TIMESTAMP_HEADER",
    "RELAY_TIMESTAMP_TOLERANCE",
    "ParsedRelaySignature",
    "RelayHeaders",
    "RelaySignatureRefusal",
    "RelaySignatureVerification",
    "matches_relay_signature",
    "parse_relay_signature",
    "sign_relayed_request",
    "verify_relay_signature",
]
