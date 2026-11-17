# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typeform signature verification and typed form response decoding."""

import base64
import binascii
import hashlib
import hmac
import json
import re
from dataclasses import replace
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import TriggerEvent
from dex_connectors.sdk.webhooktrigger import WebhookRequest, WebhookVerificationUnavailableError

from .models import FormResponseEvent, ResponseSubmittedTriggerConfiguration
from .operations import _response

_ID = re.compile(r"[A-Za-z0-9_-]{1,128}", re.ASCII)
_SIGNATURE = re.compile(r"[A-Za-z0-9+/]{43}=", re.ASCII)


def validate_response_submitted_configuration(configuration: ResponseSubmittedTriggerConfiguration) -> None:
    """Require a blank filter or one Typeform form ID."""
    if configuration.form_id and _ID.fullmatch(configuration.form_id) is None:
        raise ValueError("responseSubmitted formId is invalid")


def verify_typeform_signature(body: bytes, header: str, secret: str) -> None:
    """Compare Typeform's prefixed base64 HMAC-SHA256 over the raw request body."""
    if not secret:
        raise WebhookVerificationUnavailableError()
    signature = header.strip()
    if not signature.startswith("sha256=") or _SIGNATURE.fullmatch(signature[7:]) is None:
        raise ValueError("Typeform-Signature is missing or malformed")
    try:
        received = base64.b64decode(signature[7:], validate=True)
    except binascii.Error as error:
        raise ValueError("Typeform-Signature is malformed") from error
    expected = hmac.new(secret.encode(), body, hashlib.sha256).digest()
    if not hmac.compare_digest(received, expected):
        raise ValueError("Typeform-Signature does not match")


def decode_typeform_request(request: WebhookRequest) -> TriggerEvent[FormResponseEvent] | None:
    """Decode one signed form_response, dropping other event types without an inbox record."""
    try:
        envelope = json.loads(request.body.decode("utf-8", errors="replace"))
        if (
            not isinstance(envelope, dict)
            or not isinstance(envelope.get("event_type"), str)
            or not envelope["event_type"]
        ):
            raise ValueError("invalid Typeform webhook")
        if envelope["event_type"] != "form_response":
            return None
        submission = envelope.get("form_response")
        if not isinstance(submission, dict):
            raise ValueError("invalid Typeform webhook")
        form_id = submission.get("form_id")
        if not isinstance(form_id, str) or _ID.fullmatch(form_id) is None:
            raise ValueError("invalid Typeform form ID")
        definition = submission.get("definition")
        field_definitions: dict[str, tuple[str, str]] = {}
        form_title = ""
        if definition is not None:
            if not isinstance(definition, dict) or definition.get("id", "") not in ("", form_id):
                raise ValueError("invalid Typeform form definition")
            form_title = _wire_string(definition.get("title"))
            fields = definition.get("fields") or []
            if not isinstance(fields, list):
                raise ValueError("invalid Typeform fields")
            for field in fields:
                if not isinstance(field, dict):
                    raise ValueError("invalid Typeform field")
                field_definitions[_wire_string(field.get("id"))] = (
                    _wire_string(field.get("ref")),
                    _wire_string(field.get("title")),
                )
        response = _response(submission)
        if response.submitted_at == datetime.min.replace(tzinfo=UTC):
            raise ValueError("missing Typeform submission time")
        answers = []
        for answer in response.answers:
            definition_ref, definition_title = field_definitions.get(answer.field_id, ("", ""))
            answers.append(
                replace(answer, field_ref=answer.field_ref or definition_ref, field_title=definition_title)
            )
        response = replace(response, answers=tuple(answers))
        webhook_event_id = _wire_string(envelope.get("event_id"))
        event = FormResponseEvent(
            webhook_event_id=webhook_event_id, form_id=form_id, form_title=form_title, response=response
        )
        return TriggerEvent(f"{form_id}:{response.token}", response.submitted_at, event)
    except (TypeError, ValueError, KeyError, AttributeError) as error:
        raise ValueError("invalid Typeform form_response webhook") from error


def _wire_string(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("invalid Typeform string")
    return value
