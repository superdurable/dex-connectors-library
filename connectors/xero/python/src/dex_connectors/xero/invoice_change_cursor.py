# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The invoiceChanged cursor document, encoded and decoded exactly as the Go connector's encoding/json does.

Go and Python replicas of one binding share this cursor, so the token is Go's JSON document byte for byte.
"""

import json
import re
from dataclasses import dataclass, field
from typing import Any, Final

from dex_connectors.sdk.pollingtrigger import Cursor
from dex_connectors.sdk.projectconfig import encode_go_json_value

INVOICE_CHANGE_OVERLAP_MILLISECONDS: Final = 5_000
"""How far before the newest delivered UpdatedDateUTC each poll rereads.

If-Modified-Since has one-second precision and a change may become visible after a later one.
"""
MAXIMUM_SEEN_INVOICE_CHANGES: Final = 2_000
"""The most delivered changes the cursor remembers inside the overlap window, under the kit's 64 KiB."""

_INT64_MINIMUM, _INT64_MAXIMUM = -(1 << 63), (1 << 63) - 1
_LONE_SURROGATE: Final = re.compile("[\ud800-\udfff]")


def _go_string(value: str) -> str:
    """Replace an escaped lone surrogate with U+FFFD, as Go's encoding/json decodes it."""
    return _LONE_SURROGATE.sub("\ufffd", value)


class InvoiceChangeCursorInvalidError(ValueError):
    """An invoiceChanged cursor token that Go's encoding/json would not decode into a usable cursor."""


@dataclass(slots=True)
class InvoiceChangeCursor:
    """The committed cursor, in the Go struct's field order.

    since is the newest UpdatedDateUTC delivered, in Unix milliseconds; seen lists changes already
    delivered inside the overlap window; page is set only while more than one page of invoices changed
    within that window.
    """

    filter: str = ""
    tenant_id: str = ""
    since: int = 0
    page: int = 0
    seen: list[str] = field(default_factory=list)


def invoice_change_window_start(since_milliseconds: int) -> int:
    """Return the If-Modified-Since instant in Unix milliseconds: the overlap before since, to the second."""
    return (since_milliseconds - INVOICE_CHANGE_OVERLAP_MILLISECONDS) // 1000 * 1000


def prune_seen_invoice_changes(keys: list[str], since_milliseconds: int) -> list[str]:
    """Keep keys inside the window of since, sorted and unique, newest MAXIMUM_SEEN_INVOICE_CHANGES first.

    An evicted change can be delivered again with its same Trigger event ID, which the target
    deduplicates.
    """
    floor = f"{invoice_change_window_start(since_milliseconds):013d}"
    kept = sorted({key for key in keys if key >= floor})
    return kept[-MAXIMUM_SEEN_INVOICE_CHANGES:] if kept else []


def encode_invoice_change_cursor(position: InvoiceChangeCursor) -> Cursor:
    """Encode the cursor as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings."""
    members = ['"filter":' + encode_go_json_value(position.filter).decode()]
    if position.tenant_id:
        members.append('"tenantId":' + encode_go_json_value(position.tenant_id).decode())
    members.append(f'"since":{position.since}')
    if position.page:
        members.append(f'"page":{position.page}')
    if position.seen:
        members.append('"seen":' + encode_go_json_value(position.seen).decode())
    return Cursor(token="{" + ",".join(members) + "}")


class _JSONObject(list[tuple[str, Any]]):
    """A decoded JSON object as its ordered members, distinct from a JSON array."""


class _JSONFloat(str):
    """A JSON number with a fraction or exponent, which Go never decodes into an integer field."""


# Go's encoding/json folds each non-ASCII rune to the smallest rune of its Unicode simple fold orbit; only the
# long s and the Kelvin sign fold to ASCII letters, so no other non-ASCII rune can match an ASCII field name.
_NON_ASCII_FOLDS: Final = {"ſ": "S", "K": "K"}


def _go_folded_name(name: str) -> str:
    """Fold a key as Go's encoding/json does, as far as matching the cursor's ASCII field names needs."""
    return "".join(
        character.upper() if character.isascii() else _NON_ASCII_FOLDS.get(character, character)
        for character in name
    )


_FIELD_BY_FOLDED_NAME: Final = {
    _go_folded_name(name): attribute
    for name, attribute in (
        ("filter", "filter"),
        ("tenantId", "tenant_id"),
        ("since", "since"),
        ("page", "page"),
        ("seen", "seen"),
    )
}


def _reject_json_constant(constant: str) -> None:
    raise InvoiceChangeCursorInvalidError("Xero invoice cursor is not decodable")


def _decode_member(position: InvoiceChangeCursor, attribute: str, value: Any) -> bool:
    """Store one non-null member as Go would; return False for a value of the wrong JSON type."""
    if attribute in ("filter", "tenant_id"):
        if type(value) is not str:
            return False
        setattr(position, attribute, _go_string(value))
    elif attribute in ("since", "page"):
        if type(value) is not int or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
            return False
        setattr(position, attribute, value)
    else:
        if not isinstance(value, list) or isinstance(value, _JSONObject):
            return False
        if any(item is not None and type(item) is not str for item in value):
            return False
        position.seen = [_go_string(item) if item is not None else "" for item in value]
    return True


def decode_invoice_change_cursor(token: str) -> InvoiceChangeCursor:
    """Decode a token as Go's json.Unmarshal fills the cursor struct.

    Keys match exactly or case-insensitively, the last duplicate wins, unknown keys and null values are
    ignored, and a value of the wrong JSON type rejects the token. Raises InvoiceChangeCursorInvalidError.
    """
    invalid = InvoiceChangeCursorInvalidError("Xero invoice cursor is not decodable")
    try:
        document = json.loads(
            token, object_pairs_hook=_JSONObject, parse_float=_JSONFloat, parse_constant=_reject_json_constant
        )
    except (ValueError, RecursionError):
        raise invalid from None
    position = InvoiceChangeCursor()
    if document is None:
        return position
    if not isinstance(document, _JSONObject):
        raise invalid
    for name, value in document:
        attribute = _FIELD_BY_FOLDED_NAME.get(_go_folded_name(name))
        if attribute is None or value is None:
            continue
        if not _decode_member(position, attribute, value):
            raise invalid
    return position
