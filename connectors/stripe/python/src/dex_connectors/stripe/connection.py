# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bind Stripe operations to one static Dex connection identity."""

from dataclasses import dataclass
from typing import Protocol

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query
from dex_connectors.sdk.projectconfig import LoadedProject

from .client import Client
from .models import (
    CheckoutSession,
    CreateACHCheckoutSessionInput,
    CreateCardCheckoutSessionInput,
    CreateRefundInput,
    GetCheckoutSessionInput,
    GetPaymentIntentInput,
    PaymentIntent,
    Refund,
)
from .operations import (
    CREATE_ACH_CHECKOUT_SESSION_DEFINITION,
    CREATE_CARD_CHECKOUT_SESSION_DEFINITION,
    CREATE_REFUND_DEFINITION,
    GET_CHECKOUT_SESSION_DEFINITION,
    GET_PAYMENT_INTENT_DEFINITION,
)
from .project import new_project_client


class Operations(Protocol):
    """The five typed Stripe operations available to application Steps."""

    def create_card_checkout_session(self) -> Mutation[CreateCardCheckoutSessionInput, CheckoutSession]:
        """Return the card Checkout mutation."""
        ...

    def create_ach_checkout_session(self) -> Mutation[CreateACHCheckoutSessionInput, CheckoutSession]:
        """Return the ACH Checkout mutation."""
        ...

    def get_checkout_session(self) -> Query[GetCheckoutSessionInput, CheckoutSession]:
        """Return the Checkout Session query."""
        ...

    def get_payment_intent(self) -> Query[GetPaymentIntentInput, PaymentIntent]:
        """Return the PaymentIntent query."""
        ...

    def create_refund(self) -> Mutation[CreateRefundInput, Refund]:
        """Return the refund mutation."""
        ...


@dataclass(frozen=True, slots=True)
class Connection:
    """Stripe operations and a safe logical reference bound before Worker registration.

    The caller closes a provider-backed connection after its Steps stop. A mock
    connection owns no transport and can be closed without side effects.
    """

    operations: Operations
    reference: ConnectionRef
    _client: Client | None = None

    def __post_init__(self) -> None:
        """Reject a missing operation or a connection for another provider."""
        if self.operations is None or self.reference is None:
            raise ValueError("Stripe operations and reference are required")
        self.reference.validate()
        if self.reference.provider != "stripe":
            raise ValueError("Stripe connection provider must be stripe")
        expected = (
            (self.operations.create_card_checkout_session(), CREATE_CARD_CHECKOUT_SESSION_DEFINITION),
            (self.operations.create_ach_checkout_session(), CREATE_ACH_CHECKOUT_SESSION_DEFINITION),
            (self.operations.get_checkout_session(), GET_CHECKOUT_SESSION_DEFINITION),
            (self.operations.get_payment_intent(), GET_PAYMENT_INTENT_DEFINITION),
            (self.operations.create_refund(), CREATE_REFUND_DEFINITION),
        )
        if any(
            operation.definition().operation != definition.operation for operation, definition in expected
        ):
            raise ValueError("Stripe connection operation identity differs")

    async def aclose(self) -> None:
        """Close this connection's owned provider client after all calls finish."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Avoid including the provider client or credential source in logs."""
        return "stripe.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Stripe connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind an existing client and Stripe reference for operation Steps.

    The caller retains ownership of the supplied client and closes it after
    the Worker stops.
    """
    if client is None:
        raise ValueError("Stripe client is required")
    return Connection(client, reference, client)


def new_connection_with_operations(operations: Operations, reference: ConnectionRef) -> Connection:
    """Bind mock operations to the same identity as a provider connection."""
    return Connection(operations, reference)


def new_project_connection(
    project: LoadedProject,
    connection_name: str,
    http_client: httpx.AsyncClient | None = None,
) -> Connection:
    """Open a declared Stripe connection with call-time private credentials.

    Close the returned connection after all Worker calls finish. An injected
    HTTP client remains owned by its caller.
    """
    client = new_project_client(project, connection_name, http_client)
    return new_connection(client, ConnectionRef("stripe", connection_name))
