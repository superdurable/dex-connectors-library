# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

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
