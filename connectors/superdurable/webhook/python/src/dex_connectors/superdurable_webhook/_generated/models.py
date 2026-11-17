# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from .timestamps import parse_go_datetime, validate_go_datetime

# SendEventInput is one JSON event that sendEvent POSTs to the connection's deliveryUrl.
@dataclass(frozen=True, slots=True)
class SendEventInput:
    # Payload is the complete JSON request body, such as {"type":"form.submitted","data":{...}}. It must be
    # one valid JSON value of at most maxBodyBytes bytes; an empty or invalid payload selects defect.
    payload: Any = None


setattr(SendEventInput, '__module__', 'dex_connectors.superdurable_webhook.models')


# SendEventOutput describes one sendEvent attempt's outcome. It never holds the payload or response body.
@dataclass(frozen=True, slots=True)
class SendEventOutput:
    # WebhookID is the webhook-id header value. It is derived from the Step's idempotency key, so every
    # retry and duplicate dispatch of one Step execution sends the same ID and receivers can deduplicate.
    webhook_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'webhookId'})
    # StatusCode is the receiver's HTTP status, or zero when no response arrived.
    status_code: int = _dataclass_field(default=0, metadata={'dex_json_name': 'statusCode', 'dex_json_omit_default': True})


setattr(SendEventOutput, '__module__', 'dex_connectors.superdurable_webhook.models')


# WebhookRequestEvent is one verified webhook request. The Trigger event's ID is the request's event
# ID: webhook-id for Standard Webhooks, otherwise eventIdHeader, then eventIdPointer, then the hex
# SHA-256 of the body. Exactly one of JSONBody and FormBody is set.
@dataclass(frozen=True, slots=True)
class WebhookRequestEvent:
    # ContentType is the request media type without parameters: ContentTypeJSON or ContentTypeForm.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType'})
    # Headers holds the configured forwardedHeaders present on the request, keyed by canonical name.
    # Repeated values are joined with ", ". It never holds a credential or signature header.
    headers: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # JSONBody is the unchanged body of an application/json request.
    json_body: Any = _dataclass_field(default=None, metadata={'dex_json_name': 'jsonBody', 'dex_json_omit_default': True})
    # FormBody is the decoded body of an application/x-www-form-urlencoded request.
    form_body: dict[str, tuple[str, ...]] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'formBody', 'dex_json_omit_default': True})
    # ReceivedAt is when the endpoint read the request, in UTC. It is also the Trigger event's OccurredAt.
    received_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'receivedAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.received_at, str):
            object.__setattr__(self, 'received_at', parse_go_datetime(self.received_at, 'received_at'))
        elif self.received_at is not None:
            validate_go_datetime(self.received_at, 'received_at')


setattr(WebhookRequestEvent, '__module__', 'dex_connectors.superdurable_webhook.models')


# RequestReceivedTriggerConfiguration filters the events one requestReceived binding accepts. An empty
# configuration accepts every verified event. An event the binding rejects is still answered 200.
# NewRequestReceivedTrigger panics for an invalid configuration, so call Validate on untrusted values.
@dataclass(frozen=True, slots=True)
class RequestReceivedTriggerConfiguration:
    # MatchPointer is an RFC 6901 JSON Pointer into the event body, such as /event_type; in a form body,
    # /field/0 names the first value of field. Empty accepts every event.
    match_pointer: str = _dataclass_field(default='', metadata={'dex_json_name': 'matchPointer', 'dex_json_omit_default': True})
    # MatchValues lists the accepted values at MatchPointer. A string matches its text, and a number or
    # boolean matches its literal JSON text, such as 42 or true. It is required with MatchPointer.
    match_values: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'matchValues', 'dex_json_omit_default': True})


setattr(RequestReceivedTriggerConfiguration, '__module__', 'dex_connectors.superdurable_webhook.models')
