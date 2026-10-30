# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start input and safe persistence definitions for the card Checkout Flow."""

from dataclasses import dataclass, field, replace

import dex
from dex_connectors.sdk import MutationResult, StepAnnotations

from dex_connectors.stripe import (
    CheckoutSession,
    Connection,
    CreateCardCheckoutSessionInput,
    new_create_card_checkout_session_step,
)

CONNECTION_NAME = "stripe-payments"
FLOW_TYPE = "StripeCreateCardCheckoutSessionPython"


@dataclass(frozen=True, slots=True)
class CardCheckoutInput:
    """Customer and product details entered through Dex Web Start Flow."""

    client_reference_id: str = field(metadata={"dex_json_name": "clientReferenceId"})
    customer_email: str = field(metadata={"dex_json_name": "customerEmail"})
    success_url: str = field(metadata={"dex_json_name": "successUrl"})
    cancel_url: str = field(metadata={"dex_json_name": "cancelUrl"})
    currency: str
    unit_amount: int = field(metadata={"dex_json_name": "unitAmount"})
    product_name: str = field(metadata={"dex_json_name": "productName"})

    def to_operation_input(self) -> CreateCardCheckoutSessionInput:
        """Map the durable start value to the connector's typed operation input."""
        return CreateCardCheckoutSessionInput(
            client_reference_id=self.client_reference_id,
            customer_email=self.customer_email,
            success_url=self.success_url,
            cancel_url=self.cancel_url,
            currency=self.currency,
            unit_amount=self.unit_amount,
            product_name=self.product_name,
        )


card_checkout_request = dex.Attribute("card-checkout-request", CardCheckoutInput)
card_checkout_session = dex.Attribute("card-checkout-session", CheckoutSession)


# dex:group group-id:card-checkout group-label:"Card Checkout"
# dex:explanation text:"Validate and retain the card Checkout request."
class PrepareCardCheckout(dex.Step[CardCheckoutInput]):
    """Record the trimmed request before invoking Stripe."""

    def __init__(self, create_step: dex.Step[CardCheckoutInput]) -> None:
        self._create_step = create_step

    def execute(self, context: dex.Context, input: CardCheckoutInput) -> dex.StepDecision:
        """Persist a safe request and continue to the provider Mutation."""
        prepared = replace(
            input,
            client_reference_id=input.client_reference_id.strip(),
            customer_email=input.customer_email.strip(),
            success_url=input.success_url.strip(),
            cancel_url=input.cancel_url.strip(),
            currency=input.currency.strip().lower(),
            product_name=input.product_name.strip(),
        )
        if (
            not prepared.client_reference_id
            or not prepared.customer_email
            or not prepared.success_url
            or not prepared.cancel_url
            or not prepared.currency
            or not prepared.product_name
            or prepared.unit_amount < 1
        ):
            return dex.force_fail("all card Checkout fields are required and unitAmount must be positive")
        card_checkout_request.set(context, prepared)
        return dex.go_to(self._create_step, prepared)


# dex:group group-id:card-checkout group-label:"Card Checkout"
# dex:explanation text:"Retain the safe card Checkout result."
class CompleteCardCheckout(dex.Step[MutationResult[CheckoutSession]]):
    """Persist the safe provider result and complete the Flow."""

    def execute(
        self, context: dex.Context, input: MutationResult[CheckoutSession]
    ) -> dex.StepDecision:
        """Save the created session for Dex Web and return it to the caller."""
        card_checkout_session.set(context, input.value)
        return dex.graceful_complete(input.value)


class CardCheckoutFlow(dex.Flow[CardCheckoutInput]):
    """Create one hosted Stripe card Checkout Session."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Stripe connection at Worker registration."""
        if connection is None:
            raise ValueError("Stripe connection is required")
        self.complete = CompleteCardCheckout()
        self.create = new_create_card_checkout_session_step(
            step_type="CreateCardCheckout",
            step_input_type=CardCheckoutInput,
            annotations=StepAnnotations(
                group_id="card-checkout",
                group_label="Card Checkout",
                explanation="Create one hosted card Checkout Session.",
            ),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=CardCheckoutInput.to_operation_input,
            created=self.complete,
        )
        self.prepare = PrepareCardCheckout(self.create)

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[CardCheckoutInput]:
        """Run validation, the provider call, then safe result persistence."""
        return dex.StepList.start_step(self.prepare).other_steps(self.create, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register both safe Attributes used by the Flow."""
        return dex.PersistenceSchema.of(card_checkout_request, card_checkout_session)
