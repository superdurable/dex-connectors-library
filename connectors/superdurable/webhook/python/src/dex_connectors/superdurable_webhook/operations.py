# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Invoke the signed Standard Webhooks sendEvent operation."""

from __future__ import annotations

import base64
import hashlib
import hmac
import json

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    Receipt,
)
from dex_connectors.sdk.projectconfig.broker import CredentialBrokerUnavailableError
from dex_connectors.sdk.providerhttp import ProviderTransportError, parse_retry_after, send_traced

from ._generated.definitions import SEND_EVENT_DEFINITION
from .client import Client
from .models import SendEventInput, SendEventOutput


class SendEventOperation:
    """Deliver JSON with a stable webhook ID and fresh signature per attempt."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated sendEvent Mutation definition."""
        return SEND_EVENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendEventInput) -> IdempotencyKey:
        """Derive Standard Webhooks' msg_ ID from the stable Dex call ID."""
        return IdempotencyKey("msg_" + str(call_id).replace("-", ""))

    async def invoke(self, call: Call, input: SendEventInput) -> MutationAttempt[SendEventOutput]:
        """Send once and classify definite, retryable, and local outcomes."""
        webhook_id = str(call.idempotency_key)
        output = SendEventOutput(webhook_id=webhook_id)
        if not self.client.delivery_url:
            return _defect(output, FailureKind.VALIDATION, "the connection has no deliveryUrl")
        try:
            payload = json.dumps(
                input.payload, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False
            ).encode()
        except (TypeError, ValueError, UnicodeEncodeError):
            return _defect(output, FailureKind.VALIDATION, "sendEvent payload must be one valid JSON value")
        if input.payload is None:
            return _defect(output, FailureKind.VALIDATION, "sendEvent payload must be one valid JSON value")
        if len(payload) > self.client.max_body_bytes:
            return _defect(output, FailureKind.VALIDATION, "sendEvent payload exceeds maxBodyBytes")
        try:
            credentials = await self.client.credentials.resolve(call)
        except CredentialBrokerUnavailableError:
            return MutationAttempt.retry(_failure(FailureKind.AVAILABILITY, "credential broker unavailable"))
        except (OSError, ValueError, LookupError):
            return _defect(output, FailureKind.AUTHENTICATION, "webhook signing secret unavailable")
        secret = credentials.signing_secret.reveal()
        if not secret:
            return _defect(output, FailureKind.AUTHENTICATION, "webhook signing secret unavailable")
        if secret.startswith("whsec_"):
            try:
                key = base64.b64decode(secret[6:], validate=True)
            except ValueError:
                return _defect(output, FailureKind.VALIDATION, "whsec_ signing secret is invalid")
            if not key:
                return _defect(output, FailureKind.VALIDATION, "whsec_ signing secret is invalid")
        else:
            key = secret.encode()
        timestamp = str(int(self.client.clock().timestamp()))
        digest = hmac.new(
            key, webhook_id.encode() + b"." + timestamp.encode() + b"." + payload, hashlib.sha256
        ).digest()
        headers = {
            "Content-Type": "application/json",
            "webhook-id": webhook_id,
            "webhook-timestamp": timestamp,
            "webhook-signature": "v1," + base64.b64encode(digest).decode(),
        }
        try:
            request = self.client.http_client.build_request(
                "POST", self.client.delivery_url, headers=headers, content=payload
            )
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError as error:
            kind = FailureKind.AVAILABILITY if error.provably_not_sent else FailureKind.TRANSPORT
            return MutationAttempt.retry(_failure(kind, "receiver did not answer the event"))
        status = response.status_code
        await response.aclose()
        output = SendEventOutput(webhook_id, status)
        if 200 <= status < 300:
            return MutationAttempt.branch_result("delivered", output, Receipt(provider_object_id=webhook_id))
        delay = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
        if status in (408, 429):
            return MutationAttempt.retry(
                _failure(FailureKind.RATE_LIMIT, f"receiver answered HTTP {status}"), delay
            )
        if status == 409:
            return MutationAttempt.retry(_failure(FailureKind.CONFLICT, "receiver answered HTTP 409"), delay)
        if status == 425 or status >= 500:
            return MutationAttempt.retry(
                _failure(FailureKind.AVAILABILITY, f"receiver answered HTTP {status}"), delay
            )
        kind = {
            401: FailureKind.AUTHENTICATION,
            403: FailureKind.AUTHORIZATION,
            404: FailureKind.NOT_FOUND,
            410: FailureKind.NOT_FOUND,
        }.get(status, FailureKind.PROVIDER_REJECTION)
        return MutationAttempt.branch_result(
            "rejected",
            output,
            Receipt(provider_object_id=webhook_id),
            _failure(kind, f"receiver answered HTTP {status}"),
        )


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "webhook", "sendEvent", message)


def _defect(output: SendEventOutput, kind: FailureKind, message: str) -> MutationAttempt[SendEventOutput]:
    return MutationAttempt.branch_result("defect", output, Receipt(), _failure(kind, message))
