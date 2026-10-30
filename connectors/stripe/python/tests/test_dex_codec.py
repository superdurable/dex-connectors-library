# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the connector's Go-compatible wire values with released Dex."""

import json
from datetime import UTC, datetime
from typing import TypedDict

import pytest
from dex import CodecRegistry, Context, Value, WireKind

from dex_connectors.stripe.models import (
    CheckoutSession,
    CreateACHCheckoutSessionInput,
    CreateCardCheckoutSessionInput,
    CreateRefundInput,
    GetCheckoutSessionInput,
    GetPaymentIntentInput,
    PaymentIntent,
    Refund,
)


class _CheckoutFields(TypedDict):
    client_reference_id: str
    customer_email: str
    success_url: str
    cancel_url: str
    currency: str
    unit_amount: int
    product_name: str


_CHECKOUT_FIELDS: _CheckoutFields = {
    "client_reference_id": "order-123",
    "customer_email": "buyer@example.com",
    "success_url": "https://example.com/success",
    "cancel_url": "https://example.com/cancel",
    "currency": "usd",
    "unit_amount": 2500,
    "product_name": "Ticket",
}
_CHECKOUT_WIRE = {
    "clientReferenceId": "order-123",
    "customerEmail": "buyer@example.com",
    "successUrl": "https://example.com/success",
    "cancelUrl": "https://example.com/cancel",
    "currency": "usd",
    "unitAmount": 2500,
    "productName": "Ticket",
}


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        (CreateCardCheckoutSessionInput(**_CHECKOUT_FIELDS), _CHECKOUT_WIRE),
        (CreateACHCheckoutSessionInput(**_CHECKOUT_FIELDS), _CHECKOUT_WIRE),
        (GetCheckoutSessionInput("cs_test_123"), {"sessionId": "cs_test_123"}),
        (GetPaymentIntentInput("pi_test_123"), {"paymentIntentId": "pi_test_123"}),
        (CreateRefundInput("pi_test_123", 500), {"paymentIntentId": "pi_test_123", "amount": 500}),
        (CheckoutSession(id="cs_test_123"), {"id": "cs_test_123"}),
        (
            PaymentIntent("pi_test_123", 2500, "usd", "succeeded"),
            {
                "id": "pi_test_123",
                "amount": 2500,
                "currency": "usd",
                "status": "succeeded",
                "amountRefunded": 0,
            },
        ),
        (
            Refund("re_test_123", "succeeded", 500, "usd"),
            {"refundId": "re_test_123", "status": "succeeded", "amount": 500, "currency": "usd"},
        ),
    ],
)
def test_operation_models_match_go_wire_json(value: object, expected: dict[str, object]) -> None:
    codec = CodecRegistry().resolve(type(value))

    encoded = codec.encode(value)

    assert isinstance(encoded.data, (str, bytes))
    assert json.loads(encoded.data) == expected
    assert codec.decode(encoded) == value


def test_datetime_and_optional_fields_match_go_wire_json() -> None:
    input_value = CreateCardCheckoutSessionInput(
        **_CHECKOUT_FIELDS,
        metadata={"order": "123"},
        expires_at=datetime(2026, 10, 30, 1, 2, 3, 120000, UTC),
    )
    input_codec = CodecRegistry().resolve(CreateCardCheckoutSessionInput)
    session_codec = CodecRegistry().resolve(CheckoutSession)

    encoded_input = input_codec.encode(input_value)
    encoded_session = session_codec.encode(
        CheckoutSession(
            id="cs_test_123",
            expires_at=datetime(2026, 10, 30, 1, 2, 3, tzinfo=UTC),
            payment_intent_id="pi_test_123",
        )
    )

    assert isinstance(encoded_input.data, (str, bytes))
    assert isinstance(encoded_session.data, (str, bytes))
    assert json.loads(encoded_input.data) == {
        **_CHECKOUT_WIRE,
        "metadata": {"order": "123"},
        "expiresAt": "2026-10-30T01:02:03.12Z",
    }
    assert json.loads(encoded_session.data) == {
        "id": "cs_test_123",
        "expiresAt": "2026-10-30T01:02:03Z",
        "paymentIntentId": "pi_test_123",
    }
    assert input_codec.decode(encoded_input) == input_value
    assert session_codec.decode(encoded_session).expires_at == datetime(2026, 10, 30, 1, 2, 3, tzinfo=UTC)
    assert session_codec.decode(
        Value(WireKind.JSON, '{"id":"cs_test_123","expiresAt":"2026-10-30T01:02:03+00:00"}')
    ).expires_at == datetime(2026, 10, 30, 1, 2, 3, tzinfo=UTC)


def test_dex_context_exposes_absolute_attempt_and_flow_times() -> None:
    assert isinstance(Context.deadline, property)
    assert isinstance(Context.flow_started_at, property)
