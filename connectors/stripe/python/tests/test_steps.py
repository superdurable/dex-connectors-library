# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify public Stripe Step factories and static connection identities."""

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, StepAnnotations

from dex_connectors.stripe import (
    Client,
    Config,
    CreateACHCheckoutSessionInput,
    CreateCardCheckoutSessionInput,
    CreateRefundInput,
    Credentials,
    GetCheckoutSessionInput,
    GetPaymentIntentInput,
    new_connection,
    new_create_ach_checkout_session_step,
    new_create_card_checkout_session_step,
    new_create_refund_step,
    new_get_checkout_session_step,
    new_get_payment_intent_step,
)
from dex_connectors.stripe.stripemock import Mock


async def test_all_stripe_factories_bind_stable_step_types() -> None:
    """Construct each operation-specific Step with only its required happy path."""
    reference = ConnectionRef("stripe", "stripe-payments")
    credentials = StaticCredentialProvider({reference: Credentials(SecretString("sk_test_inert"))})
    annotations = StepAnnotations("stripe", "Stripe", "Call Stripe for one typed operation.")
    target = dex.StepRef("Sink")
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(500))) as http:
        connection = new_connection(Client(Config(), credentials, http), reference)
        steps = (
            new_create_card_checkout_session_step(
                step_type="CreateCardCheckout",
                step_input_type=str,
                annotations=annotations,
                connection=connection,
                connection_name=reference.name,
                map_to_operation_input=_card_input,
                created=target,
            ),
            new_create_ach_checkout_session_step(
                step_type="CreateACHCheckout",
                step_input_type=str,
                annotations=annotations,
                connection=connection,
                connection_name=reference.name,
                map_to_operation_input=_ach_input,
                created=target,
            ),
            new_get_checkout_session_step(
                step_type="GetCheckoutSession",
                step_input_type=str,
                annotations=annotations,
                connection=connection,
                connection_name=reference.name,
                map_to_operation_input=GetCheckoutSessionInput,
                found=target,
            ),
            new_get_payment_intent_step(
                step_type="GetPaymentIntent",
                step_input_type=str,
                annotations=annotations,
                connection=connection,
                connection_name=reference.name,
                map_to_operation_input=GetPaymentIntentInput,
                found=target,
            ),
            new_create_refund_step(
                step_type="CreateRefund",
                step_input_type=str,
                annotations=annotations,
                connection=connection,
                connection_name=reference.name,
                map_to_operation_input=_refund_input,
                refunded=target,
            ),
        )

    assert [step.get_step_type() for step in steps] == [
        "CreateCardCheckout",
        "CreateACHCheckout",
        "GetCheckoutSession",
        "GetPaymentIntent",
        "CreateRefund",
    ]
    assert all(step.get_input_type() is str for step in steps)
    assert all(step.get_step_options().execute_durability == dex.StepDurability.ASYNC for step in steps)


async def test_factory_rejects_a_different_static_connection_name() -> None:
    """Fail Worker registration before it can use the wrong Dex Web connection."""
    reference = ConnectionRef("stripe", "stripe-payments")
    credentials = StaticCredentialProvider({reference: Credentials(SecretString("sk_test_inert"))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(500))) as http:
        connection = new_connection(Client(Config(), credentials, http), reference)
        with pytest.raises(ValueError, match="must equal"):
            new_get_payment_intent_step(
                step_type="GetPaymentIntent",
                step_input_type=str,
                annotations=StepAnnotations("stripe", "Stripe", "Read one PaymentIntent."),
                connection=connection,
                connection_name="wrong-connection",
                map_to_operation_input=GetPaymentIntentInput,
                found=dex.StepRef("Sink"),
            )


def test_mock_connection_binds_a_step_factory() -> None:
    """Application Steps use the scripted mock operation instead of HTTP."""
    mock = Mock("stripe-payments")
    connection = mock.connection()
    step = new_create_card_checkout_session_step(
        step_type="CreateCardCheckout",
        step_input_type=str,
        annotations=StepAnnotations("stripe", "Stripe", "Create a card Checkout Session."),
        connection=connection,
        connection_name="stripe-payments",
        map_to_operation_input=_card_input,
        created=dex.StepRef("Sink"),
    )
    assert step.get_step_type() == "CreateCardCheckout"
    assert step._config.operation is mock.create_card_checkout_session
    assert connection.operations.create_card_checkout_session() is mock.create_card_checkout_session


def _card_input(reference: str) -> CreateCardCheckoutSessionInput:
    return CreateCardCheckoutSessionInput(
        reference,
        "buyer@example.com",
        "https://example.com/s",
        "https://example.com/c",
        "usd",
        100,
        "Ticket",
    )


def _ach_input(reference: str) -> CreateACHCheckoutSessionInput:
    return CreateACHCheckoutSessionInput(
        reference,
        "buyer@example.com",
        "https://example.com/s",
        "https://example.com/c",
        "usd",
        100,
        "Ticket",
    )


def _refund_input(payment_intent_id: str) -> CreateRefundInput:
    return CreateRefundInput(payment_intent_id, 100)
