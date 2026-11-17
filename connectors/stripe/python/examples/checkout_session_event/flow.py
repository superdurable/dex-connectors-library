# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Record each verified Stripe Checkout Session event in a Dex Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import TriggerBindingDefinition, TriggerEvent

from dex_connectors.stripe import (
    CheckoutSessionEvent,
    Connection,
    define_checkout_session_updated_trigger_binding,
)

CONNECTION_NAME = "stripe-payments"
BINDING_NAME = "checkout-events"
FLOW_ID_PREFIX = "stripe-checkout-event-"


@dataclass(frozen=True, slots=True)
class CheckoutEventInput:
    """One verified Stripe Checkout Session event and its stable ID."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    received: CheckoutSessionEvent


checkout_event_attribute = dex.Attribute("stripe-checkout-event-python", CheckoutEventInput)


def resolve_flow_id(event: TriggerEvent[CheckoutSessionEvent]) -> str:
    """Use Stripe's event ID so a retry starts at most one Flow."""
    return FLOW_ID_PREFIX + event.id


def map_to_flow_input(event: TriggerEvent[CheckoutSessionEvent]) -> CheckoutEventInput:
    """Keep Stripe's event ID beside the safe session snapshot."""
    return CheckoutEventInput(event.id, event.payload)


# dex:group group-id:checkout-event group-label:"Checkout event"
# dex:explanation text:"Record the signed Checkout Session event and complete the Flow."
class RecordCheckoutEvent(dex.Step[CheckoutEventInput]):
    """Persist a verified Checkout Session event in a Dex Attribute."""

    def get_step_type(self) -> str:
        """Keep the Step identity stable."""
        return "RecordCheckoutEvent"

    def execute(self, context: dex.Context, input: CheckoutEventInput) -> dex.StepDecision:
        """Fail incomplete input; otherwise record and complete."""
        if not input.trigger_event_id or not input.received.session.id:
            return dex.force_fail("triggerEventId and received.session.id are required")
        checkout_event_attribute.set(context, input)
        return dex.graceful_complete(input)


class CheckoutEventFlow(dex.Flow[CheckoutEventInput]):
    """Record one signed Stripe Checkout Session event per event ID."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Stripe connection."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Checkout event Flow requires the static Stripe connection")
        self.record = RecordCheckoutEvent()

    def get_flow_type(self) -> str:
        """Keep the Flow identity stable."""
        return "StripeCheckoutEventPython"

    def get_steps(self) -> dex.StepList[CheckoutEventInput]:
        """Start at the recording Step."""
        return dex.StepList.start_step(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the Checkout event Attribute."""
        return dex.PersistenceSchema.of(checkout_event_attribute)

    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_checkout_session_updated_trigger_binding(
                connection_name="stripe-payments", binding_name="checkout-events"
            )
        ]
