# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typeform signed webhook and durable Trigger contract."""

import asyncio
import base64
import hashlib
import hmac
import json
from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.projectconfig import decode_trigger_event, encode_trigger_event
from dex_connectors.sdk.webhooktrigger import WebhookRequest

from dex_connectors.typeform import (
    Client,
    Config,
    Credentials,
    FormResponseEvent,
    ResponseSubmittedTriggerConfiguration,
    new_connection,
    new_response_submitted_trigger,
    response_submitted_webhook_handler,
    validate_response_submitted_configuration,
)
from dex_connectors.typeform._webhook import decode_typeform_request, verify_typeform_signature
from dex_connectors.typeform.triggers import _decode_pending_event

REFERENCE = ConnectionRef("typeform", "forms")
SECRET = "sentinel-" + "webhook-secret"


def body(form_id: str = "lT4Z3j", token: str = "token1", kind: str = "form_response") -> bytes:
    """Encode the documented Typeform envelope with a field definition and typed answer."""
    return json.dumps(
        {
            "event_id": "delivery1",
            "event_type": kind,
            "form_response": {
                "form_id": form_id,
                "token": token,
                "submitted_at": "2026-09-30T11:58:59Z",
                "landed_at": "2026-09-30T11:50:00Z",
                "answers": [{"field": {"id": "field1", "type": "short_text"}, "type": "text", "text": "Ada"}],
                "definition": {
                    "id": form_id,
                    "title": "Signup",
                    "fields": [{"id": "field1", "ref": "first_name", "title": "First name"}],
                },
            },
        },
        separators=(",", ":"),
    ).encode()


def signature(payload: bytes, secret: str = SECRET) -> str:
    """Sign the raw body with Typeform's sha256= base64 format."""
    return "sha256=" + base64.b64encode(hmac.new(secret.encode(), payload, hashlib.sha256).digest()).decode()


class RecordingTarget:
    """Collect delivered events for endpoint tests."""

    def __init__(self) -> None:
        self.events: list[TriggerEvent[FormResponseEvent]] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[FormResponseEvent]) -> None:
        self.events.append(event)


def client(secret: str = SECRET, limit: int = 1048576) -> Client:
    """Open a connection using only in-memory stand-in credentials."""
    credentials = Credentials(access_token=SecretString("inert"), webhook_secret=SecretString(secret))
    return Client(Config(webhook_max_body_bytes=limit), StaticCredentialProvider({REFERENCE: credentials}))


async def wait_for_count(target: RecordingTarget, count: int) -> None:
    """Poll until asynchronous Trigger delivery converges."""
    for _ in range(500):
        if len(target.events) == count:
            return
        await asyncio.sleep(0.01)
    raise AssertionError("Trigger event count did not converge")


async def test_signed_submission_is_typed_and_keeps_form_token_id() -> None:
    """A different webhook delivery ID keeps the same stable Typeform response ID."""
    connection = new_connection(client(), REFERENCE)
    endpoint = response_submitted_webhook_handler(connection)
    target = RecordingTarget()
    runner = new_response_submitted_trigger(
        connection=connection,
        binding_name="submissions",
        configuration=ResponseSubmittedTriggerConfiguration(form_id="lT4Z3j"),
        target=target,
    )
    context = TriggerContext()
    task = asyncio.create_task(runner.run(context))
    try:
        for _ in range(500):
            if endpoint.running_source_count() == 1:
                break
            await asyncio.sleep(0.01)
        payload = body()
        response = await endpoint.handle_request(
            "POST", "/webhooks/typeform", [("Typeform-Signature", signature(payload))], payload
        )
        assert response.status == 200
        await wait_for_count(target, 1)
        event = target.events[0]
        assert event.id == "lT4Z3j:token1"
        assert event.occurred_at == datetime(2026, 9, 30, 11, 58, 59, tzinfo=UTC)
        assert event.payload.form_title == "Signup"
        assert event.payload.response.answers[0].field_ref == "first_name"
        assert event.payload.response.answers[0].field_title == "First name"
        assert decode_trigger_event(encode_trigger_event(event), _decode_pending_event) == event
        second = payload.replace(b'"delivery1"', b'"delivery2"')
        response = await endpoint.handle_request(
            "POST", "/webhooks/typeform", [("Typeform-Signature", signature(second))], second
        )
        assert response.status == 200
        await wait_for_count(target, 2)
        assert target.events[1].id == event.id
        assert SECRET not in repr(target.events)
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await connection.aclose()


async def test_endpoint_rejects_forgery_and_filters_another_form() -> None:
    """Signatures fail 400; other form and event type answer 200 without delivery."""
    connection = new_connection(client(limit=4096), REFERENCE)
    endpoint = response_submitted_webhook_handler(connection)
    target = RecordingTarget()
    runner = new_response_submitted_trigger(
        connection=connection,
        binding_name="submissions",
        configuration=ResponseSubmittedTriggerConfiguration(form_id="lT4Z3j"),
        target=target,
    )
    context = TriggerContext()
    task = asyncio.create_task(runner.run(context))
    try:
        for _ in range(500):
            if endpoint.running_source_count() == 1:
                break
            await asyncio.sleep(0.01)
        payload = body()
        for header in ("", signature(payload, "other"), signature(payload)[7:], "sha256=bad"):
            response = await endpoint.handle_request(
                "POST", "/webhooks/typeform", [("Typeform-Signature", header)], payload
            )
            assert response.status == 400
        for other in (body(form_id="otherForm1"), body(kind="form_response_partial")):
            response = await endpoint.handle_request(
                "POST", "/webhooks/typeform", [("Typeform-Signature", signature(other))], other
            )
            assert response.status == 200
        assert target.events == []
        huge = payload + b" " * 4096
        assert (await endpoint.handle_request("POST", "/webhooks/typeform", [], huge)).status == 413
        assert (await endpoint.handle_request("GET", "/webhooks/typeform", [], payload)).status == 405
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await connection.aclose()


def test_raw_signature_and_decoder_refuse_invalid_payload() -> None:
    """Malformed content never becomes a persisted event."""
    payload = body()
    verify_typeform_signature(payload, signature(payload), SECRET)
    with pytest.raises(ValueError):
        verify_typeform_signature(payload + b" ", signature(payload), SECRET)
    with pytest.raises(ValueError):
        decode_typeform_request(WebhookRequest("/", (), b"not-json", datetime.now(UTC)))


async def test_missing_secret_and_idle_binding_answer_503() -> None:
    """Typeform can retry while the secret or a running binding is unavailable."""
    payload = body()
    connection = new_connection(client(), REFERENCE)
    endpoint = response_submitted_webhook_handler(connection)
    response = await endpoint.handle_request(
        "POST", "/webhooks/typeform", [("Typeform-Signature", signature(payload))], payload
    )
    assert response.status == 503
    await connection.aclose()
    connection = new_connection(client(""), REFERENCE)
    endpoint = response_submitted_webhook_handler(connection)
    runner = new_response_submitted_trigger(
        connection=connection,
        binding_name="submissions",
        configuration=ResponseSubmittedTriggerConfiguration(),
        target=RecordingTarget(),
    )
    context = TriggerContext()
    task = asyncio.create_task(runner.run(context))
    try:
        for _ in range(500):
            if endpoint.running_source_count() == 1:
                break
            await asyncio.sleep(0.01)
        response = await endpoint.handle_request(
            "POST", "/webhooks/typeform", [("Typeform-Signature", signature(payload))], payload
        )
        assert response.status == 503
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await connection.aclose()
    with pytest.raises(ValueError):
        validate_response_submitted_configuration(ResponseSubmittedTriggerConfiguration(form_id="bad form"))
