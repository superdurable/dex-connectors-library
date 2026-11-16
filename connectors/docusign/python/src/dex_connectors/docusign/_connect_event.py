# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""DocuSign Connect HMAC verification and JSON SIM envelope event decoding, at parity with Go."""

import base64
import binascii
import hmac
import json
import re
from collections.abc import Iterable
from datetime import UTC, datetime
from hashlib import sha256
from typing import Any, Final

from dex_connectors.sdk import TriggerEvent

from .client import is_guid
from .models import EnvelopeCustomField, EnvelopeEvent, EnvelopeEventReceivedTriggerConfiguration
from .operations import _time

CONNECT_EVENT_ENVELOPE_COMPLETED: Final = "envelope-completed"
"""The Connect event sent when every recipient finished."""
CONNECT_EVENT_ENVELOPE_DECLINED: Final = "envelope-declined"
"""The Connect event sent when a recipient declined."""
CONNECT_EVENT_ENVELOPE_VOIDED: Final = "envelope-voided"
"""The Connect event sent when the sender voided the envelope or it expired."""
CONNECT_ENVELOPE_EVENTS: Final = (
    CONNECT_EVENT_ENVELOPE_COMPLETED,
    CONNECT_EVENT_ENVELOPE_DECLINED,
    CONNECT_EVENT_ENVELOPE_VOIDED,
)
"""The Connect events the envelopeEventReceived Trigger records, in Go's order."""

_SIGNATURE_HEADER_PREFIX: Final = "x-docusign-signature-"
_DECIMAL: Final = re.compile(r"[0-9]+\Z")
_MALFORMED: Final = "DocuSign returned a malformed response"


class ConnectMessageMalformedError(ValueError):
    """A Connect body that is not a JSON SIM envelope event with GUID identities; the endpoint answers 400."""

    def __init__(self) -> None:
        """Create the error with Go's message."""
        super().__init__(_MALFORMED)


class ConnectSignatureInvalidError(ValueError):
    """No X-DocuSign-Signature-N header matches the body's HMAC-SHA256; the endpoint answers 400."""

    def __init__(self) -> None:
        """Create the error with Go's message."""
        super().__init__("no X-DocuSign-Signature header matches the HMAC-SHA256 of the body")


def validate_envelope_event_received_trigger_configuration(
    configuration: EnvelopeEventReceivedTriggerConfiguration,
) -> None:
    """Require events to hold distinct supported Connect events; raises ValueError as Go's Validate errs."""
    seen: set[str] = set()
    for event in configuration.events:
        if event not in CONNECT_ENVELOPE_EVENTS or event in seen:
            raise ValueError(
                "envelopeEventReceived events must be distinct values from "
                + ", ".join(CONNECT_ENVELOPE_EVENTS)
            )
        seen.add(event)


def envelope_event_custom_field_value(event: EnvelopeEvent, name: str) -> str | None:
    """Return the value of the event's named text custom field, or None when it has none."""
    return next((field.value for field in event.custom_fields if field.name == name), None)


def verify_connect_signature(body: bytes, headers: Iterable[tuple[str, str]], hmac_key: str) -> None:
    """Accept any X-DocuSign-Signature-N whose standard base64 equals the body's HMAC-SHA256 under hmac_key.

    N is a 16-bit unsigned decimal, one header per account HMAC key. Raises ConnectSignatureInvalidError.
    """
    expected = hmac.new(hmac_key.encode("utf-8"), body, sha256).digest()
    for name, value in headers:
        lowered = name.lower()
        suffix = lowered[len(_SIGNATURE_HEADER_PREFIX) :]
        if (
            not lowered.startswith(_SIGNATURE_HEADER_PREFIX)
            or not _DECIMAL.match(suffix)
            or int(suffix) > 65535
        ):
            continue
        # Go's base64.StdEncoding skips CR and LF.
        encoded = value.strip(" \t\r\n\v\f").replace("\r", "").replace("\n", "")
        try:
            signature = base64.b64decode(encoded, validate=True)
        except (binascii.Error, ValueError):
            continue
        if hmac.compare_digest(signature, expected):
            return
    raise ConnectSignatureInvalidError()


def decode_connect_message(body: bytes, received_at: datetime) -> TriggerEvent[EnvelopeEvent] | None:
    """Decode a JSON SIM envelope outcome received at received_at.

    Another valid Connect event returns None, so the endpoint acknowledges it unrecorded; anything else
    raises ConnectMessageMalformedError. Keys match case-insensitively and the last one wins, as Go's
    encoding/json decodes them. The event ID is ``<envelope ID>:<event>``.
    """
    try:
        message = json.loads(body.decode("utf-8", errors="replace"), parse_constant=_reject_constant)
    except ValueError as error:
        raise ConnectMessageMalformedError() from error
    root = _object_or_none(message)
    event_name = _string_field(root, "event")
    generated_date_time = _string_field(root, "generatedDateTime")
    data = _object_or_none(_field(root, "data"))
    if event_name == "" or data is None:
        raise ConnectMessageMalformedError()
    account_id = _string_field(data, "accountId")
    envelope_id = _string_field(data, "envelopeId")
    summary = _field(data, "envelopeSummary")
    if event_name not in CONNECT_ENVELOPE_EVENTS:
        return None
    if not is_guid(envelope_id) or not is_guid(account_id):
        raise ConnectMessageMalformedError()
    generated_at = received_at.astimezone(UTC)
    if generated_date_time:
        try:
            parsed = _time(generated_date_time)
        except ValueError:
            parsed = None  # Go parity: an unparsable time keeps the receipt time.
        if parsed is not None:
            generated_at = parsed
    status: str | None = None
    voided_reason = ""
    custom_fields: tuple[EnvelopeCustomField, ...] = ()
    # The summary is an object only when the Connect configuration includes envelope data.
    if isinstance(summary, dict):
        status = _string_field(summary, "status").lower() or None
        voided_reason = _string_field(summary, "voidedReason")
        custom_fields = _text_custom_fields(_object_or_none(_field(summary, "customFields")))
    event = EnvelopeEvent(
        event=event_name,
        envelope_id=envelope_id.lower(),
        account_id=account_id.lower(),
        generated_at=generated_at,
        status=status,
        voided_reason=voided_reason,
        custom_fields=custom_fields,
    )
    return TriggerEvent(f"{event.envelope_id}:{event.event}", event.generated_at, event)


def _text_custom_fields(fields: dict[str, Any] | None) -> tuple[EnvelopeCustomField, ...]:
    raw = _field(fields, "textCustomFields")
    if raw is None:
        return ()
    if not isinstance(raw, list):
        raise ConnectMessageMalformedError()
    converted: list[EnvelopeCustomField] = []
    for entry in raw:
        custom_field = _object_or_none(entry)
        name, value = _string_field(custom_field, "name"), _string_field(custom_field, "value")
        if name:
            converted.append(EnvelopeCustomField(name, value))
    return tuple(converted)


def _reject_constant(token: str) -> None:
    raise ConnectMessageMalformedError()


def _field(value: dict[str, Any] | None, name: str) -> Any:
    if value is None:
        return None
    folded = name.lower()
    found: Any = None
    for key, member in value.items():
        if key == name or key.lower().replace("ſ", "s") == folded:
            found = member
    return found


def _string_field(value: dict[str, Any] | None, name: str) -> str:
    member = _field(value, name)
    if member is None:
        return ""
    if not isinstance(member, str):
        raise ConnectMessageMalformedError()
    return member


def _object_or_none(value: Any) -> dict[str, Any] | None:
    if value is None:
        return None
    if not isinstance(value, dict):
        raise ConnectMessageMalformedError()
    return value
