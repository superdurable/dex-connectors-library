# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The memberChanged contact versions, snapshot entries, and event identities, byte-compatible with Go."""

import hashlib
import json
import math
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from decimal import Decimal
from typing import Any, Final

from dex_connectors.sdk import TriggerEvent

from .models import Member, MemberChangedEvent, MemberChangedTriggerConfiguration

MEMBER_STATUSES: Final = ("subscribed", "unsubscribed", "cleaned", "pending", "transactional", "archived")
"""Every status the memberStatusPicker offers, in its display order."""

_RESOURCE_ID: Final = re.compile(r"[A-Za-z0-9]{1,64}\Z")
_GO_HTML_ESCAPES: Final = (
    ("<", "\\u003c"),
    (">", "\\u003e"),
    ("&", "\\u0026"),
    ("\u2028", "\\u2028"),
    ("\u2029", "\\u2029"),
)
_LONE_SURROGATE: Final = re.compile("[\ud800-\udfff]")


def validate_member_changed_trigger_configuration(configuration: MemberChangedTriggerConfiguration) -> None:
    """Require a Mailchimp audience ID and known, distinct statuses, raising ValueError as Go's Validate."""
    if not isinstance(configuration.audience_id, str) or not _RESOURCE_ID.fullmatch(
        configuration.audience_id
    ):
        raise ValueError("audienceId must be a Mailchimp ID of 1 to 64 letters and digits")
    for index, status in enumerate(configuration.statuses):
        if status not in MEMBER_STATUSES:
            raise ValueError(
                f"statuses[{index}] {_go_string_json(str(status))} is not a Mailchimp contact status"
            )
        if status in configuration.statuses[:index]:
            raise ValueError(f"statuses lists {_go_string_json(status)} twice")


def accepts_member_status(configuration: MemberChangedTriggerConfiguration, status: str) -> bool:
    """Report whether a contact in status passes the binding's status filter."""
    return not configuration.statuses or status in configuration.statuses


def _go_string_json(value: str) -> str:
    serialized = json.dumps(_LONE_SURROGATE.sub("\ufffd", value), ensure_ascii=False)
    for character, escape in _GO_HTML_ESCAPES:
        serialized = serialized.replace(character, escape)
    return serialized


def _go_float_json(value: float) -> str:
    """Encode a float64 as Go's encoding/json does: shortest digits, exponent only outside [1e-6, 1e21)."""
    if not math.isfinite(value):
        raise ValueError("a merge field number is not finite")
    if value == 0:
        return "-0" if math.copysign(1, value) < 0 else "0"
    decimal = Decimal(repr(value)).normalize()
    if 1e-6 <= abs(value) < 1e21:
        return format(decimal, "f")
    sign, digits, exponent = decimal.as_tuple()
    assert isinstance(exponent, int)
    text = "".join(str(digit) for digit in digits)
    mantissa = text[0] + ("." + text[1:] if len(text) > 1 else "")
    power = exponent + len(text) - 1
    suffix = f"e+{power:02d}" if power >= 0 else f"e-{-power}"
    return ("-" if sign else "") + mantissa + suffix


def go_json(value: Any) -> str:
    """Encode a decoded JSON value as Go marshals the same value decoded into any."""
    if value is None:
        return "null"
    if value is True:
        return "true"
    if value is False:
        return "false"
    if isinstance(value, str):
        return _go_string_json(value)
    if isinstance(value, int | float):
        # Go decodes every JSON number into a float64.
        return _go_float_json(float(value))
    if isinstance(value, list | tuple):
        return "[" + ",".join(go_json(item) for item in value) + "]"
    if isinstance(value, dict):
        keys = sorted(value, key=lambda key: _LONE_SURROGATE.sub("\ufffd", key).encode("utf-8"))
        return "{" + ",".join(f"{_go_string_json(key)}:{go_json(value[key])}" for key in keys) + "}"
    raise ValueError("a merge field value is not JSON")


def go_time_json(value: datetime) -> str:
    """Format an aware datetime as Go's time.Time MarshalJSON does in UTC (RFC 3339 with nanoseconds)."""
    instant = value.astimezone(UTC)
    fraction = f"{instant.microsecond:06d}".rstrip("0")
    return instant.strftime("%Y-%m-%dT%H:%M:%S") + ("." + fraction if fraction else "") + "Z"


def _member_json(member: Member) -> str:
    """Encode the contact as Go marshals Member: declaration order, omitempty, and sorted tags."""
    fields: list[tuple[str, str]] = [
        ("subscriberHash", _go_string_json(member.subscriber_hash)),
        ("emailAddress", _go_string_json(member.email_address)),
    ]
    if member.contact_id:
        fields.append(("contactId", _go_string_json(member.contact_id)))
    if member.full_name:
        fields.append(("fullName", _go_string_json(member.full_name)))
    fields.append(("status", _go_string_json(member.status)))
    if member.email_type:
        fields.append(("emailType", _go_string_json(member.email_type)))
    if member.merge_fields:
        fields.append(("mergeFields", go_json(member.merge_fields)))
    if member.tags:
        # Tag order is not part of the version; Go sorts by bytes.
        tags = sorted(member.tags, key=lambda tag: _LONE_SURROGATE.sub("\ufffd", tag).encode("utf-8"))
        fields.append(("tags", go_json(tags)))
    fields.append(("tagCount", str(member.tag_count)))
    if member.is_vip:
        fields.append(("isVip", "true"))
    if member.language:
        fields.append(("language", _go_string_json(member.language)))
    fields.append(("listId", _go_string_json(member.list_id)))
    if member.web_id:
        fields.append(("webId", str(member.web_id)))
    for name, moment in (
        ("signedUpAt", member.signed_up_at),
        ("optedInAt", member.opted_in_at),
        ("lastChangedAt", member.last_changed_at),
    ):
        if moment is not None:
            fields.append((name, '"' + go_time_json(moment) + '"'))
    return "{" + ",".join(f'"{name}":{encoded}' for name, encoded in fields) + "}"


def member_version_digest(audience_id: str, member: Member, size: int) -> str:
    """Hash the audience and every listed field of the contact version, as Go's memberVersionDigest."""
    canonical = _go_string_bytes(audience_id) + b"\x00" + _member_json(member).encode("utf-8")
    return hashlib.sha256(canonical).digest()[:size].hex()


def _go_string_bytes(value: str) -> bytes:
    return _LONE_SURROGATE.sub("\ufffd", value).encode("utf-8")


def member_change_id(audience_id: str, member: Member) -> str:
    """The Trigger event ID of one contact version; two versions in one second get different IDs."""
    return "mailchimp-" + member_version_digest(audience_id, member, 16)


@dataclass(frozen=True, slots=True)
class MemberSnapshotItem:
    """Keys a contact's delivered version, so rereading the lookback window delivers nothing twice."""

    audience_id: str
    member: Member

    def snapshot_key(self) -> str:
        return self.member.subscriber_hash

    def snapshot_hash(self) -> str:
        return member_version_digest(self.audience_id, self.member, 8)

    def snapshot_value(self) -> bytes | None:
        return None


def new_member_changed_trigger_event(audience_id: str, member: Member) -> TriggerEvent[MemberChangedEvent]:
    """Convert one listed contact; its ID is a pure function of the contact version."""
    assert member.last_changed_at is not None
    return TriggerEvent(
        member_change_id(audience_id, member),
        member.last_changed_at.astimezone(UTC),
        MemberChangedEvent(audience_id=audience_id, member=member),
    )
