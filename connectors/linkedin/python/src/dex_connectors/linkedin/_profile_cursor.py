# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The profileChanged cursor document, encoded and decoded as the Go connector's encoding/json does.

Go and Python replicas of one binding share this cursor, so the token is Go's JSON document byte for byte.
"""

import json
import re
from dataclasses import dataclass, field, fields, replace
from typing import Any, Final

from dex_connectors.sdk.pollingtrigger import Cursor
from dex_connectors.sdk.projectconfig import encode_go_json_value

from .models import ProfileClaims

PROFILE_CURSOR_VERSION: Final = 1
"""Versions the committed cursor token; another value records a new baseline."""

_INT64_MINIMUM, _INT64_MAXIMUM = -(1 << 63), (1 << 63) - 1
_LONE_SURROGATE: Final = re.compile("[\ud800-\udfff]")
# Go folds each non-ASCII rune within its simple fold orbit; only these two fold to ASCII letters.
_NON_ASCII_FOLDS: Final = {"ſ": "S", "K": "K"}


class ProfileCursorInvalidError(ValueError):
    """A profileChanged cursor token that Go's encoding/json would not decode."""


@dataclass(slots=True)
class ProfileCursor:
    """The committed cursor, in the Go struct's field order.

    hash covers the tracked claims of profile, the last profile seen; sequence counts delivered changes.
    """

    version: int = 0
    subject: str = ""
    hash: str = ""
    sequence: int = 0
    profile: ProfileClaims = field(default_factory=ProfileClaims)

    def is_usable(self) -> bool:
        """Report whether the cursor holds a current-version profile of one member."""
        return (
            self.version == PROFILE_CURSOR_VERSION
            and self.subject != ""
            and self.subject == self.profile.subject
            and self.hash != ""
        )


def encode_profile_cursor(position: ProfileCursor) -> Cursor:
    """Encode the cursor as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings."""
    members = [
        f'"version":{position.version}',
        '"subject":' + encode_go_json_value(position.subject).decode(),
        '"hash":' + encode_go_json_value(position.hash).decode(),
    ]
    if position.sequence:
        members.append(f'"sequence":{position.sequence}')
    members.append('"profile":' + encode_profile_claims(position.profile))
    return Cursor(token="{" + ",".join(members) + "}")


def encode_profile_claims(profile: ProfileClaims) -> str:
    """Encode the profile as Go's json.Marshal encodes ProfileClaims: field order and omitempty."""
    members = []
    for item in fields(ProfileClaims):
        value = getattr(profile, item.name)
        if item.metadata.get("dex_json_omit_default") and value == item.default:
            continue
        name = str(item.metadata.get("dex_json_name", item.name))
        members.append(f'"{name}":' + encode_go_json_value(value).decode())
    return "{" + ",".join(members) + "}"


class _JSONObject(list[tuple[str, Any]]):
    """A decoded JSON object as its ordered members, distinct from a JSON array."""


class _JSONFloat(str):
    """A JSON number with a fraction or exponent, which Go never decodes into an integer field."""


def _go_folded_name(name: str) -> str:
    """Fold a key as Go's encoding/json does, as far as matching ASCII field names needs."""
    return "".join(
        character.upper() if character.isascii() else _NON_ASCII_FOLDS.get(character, character)
        for character in name
    )


_CURSOR_FIELDS: Final = {
    _go_folded_name(name): (attribute, kind)
    for name, attribute, kind in (
        ("version", "version", int),
        ("subject", "subject", str),
        ("hash", "hash", str),
        ("sequence", "sequence", int),
        ("profile", "profile", ProfileClaims),
    )
}
_PROFILE_FIELDS: Final = {
    _go_folded_name(str(item.metadata.get("dex_json_name", item.name))): (
        item.name,
        bool if item.name == "email_verified" else str,
    )
    for item in fields(ProfileClaims)
}


def _reject_json_constant(constant: str) -> None:
    raise ProfileCursorInvalidError("LinkedIn profile cursor is not decodable")


def _decode_scalar(value: Any, kind: type) -> Any:
    """Return value as Go would store it in a field of kind; raise for another JSON type."""
    if kind is str and type(value) is str:
        return _LONE_SURROGATE.sub("�", value)
    if kind is bool and type(value) is bool:
        return value
    if kind is int and type(value) is int and _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
        return value
    raise ProfileCursorInvalidError("LinkedIn profile cursor is not decodable")


def _decode_profile(profile: ProfileClaims, document: Any) -> ProfileClaims:
    """Decode into the existing profile, as Go fills a struct field again for a repeated key."""
    if not isinstance(document, _JSONObject):
        raise ProfileCursorInvalidError("LinkedIn profile cursor is not decodable")
    updates: dict[str, Any] = {}
    for name, value in document:
        member = _PROFILE_FIELDS.get(_go_folded_name(name))
        if member is not None and value is not None:
            updates[member[0]] = _decode_scalar(value, member[1])
    return replace(profile, **updates)


def decode_profile_cursor(token: str) -> ProfileCursor:
    """Decode a token as Go's json.Unmarshal fills the cursor struct.

    Keys match exactly or case-insensitively, the last duplicate wins, unknown keys and null values are
    ignored, and a value of the wrong JSON type rejects the token. Raises ProfileCursorInvalidError.
    """
    invalid = ProfileCursorInvalidError("LinkedIn profile cursor is not decodable")
    try:
        document = json.loads(
            token, object_pairs_hook=_JSONObject, parse_float=_JSONFloat, parse_constant=_reject_json_constant
        )
    except (ValueError, RecursionError):
        raise invalid from None
    position = ProfileCursor()
    if document is None:
        return position
    if not isinstance(document, _JSONObject):
        raise invalid
    for name, value in document:
        member = _CURSOR_FIELDS.get(_go_folded_name(name))
        if member is None or value is None:
            continue
        attribute, kind = member
        if kind is ProfileClaims:
            position.profile = _decode_profile(position.profile, value)
        else:
            setattr(position, attribute, _decode_scalar(value, kind))
    return position
