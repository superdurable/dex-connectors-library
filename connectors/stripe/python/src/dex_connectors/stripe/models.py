# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed, secret-safe inputs and outputs for Stripe operations."""

from dataclasses import dataclass, field
from datetime import datetime

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """Stripe API and webhook limits from the manifest's connection settings.

    API calls use ``endpoint`` and ``max_response_bytes``. The webhook settings
    preserve the shared connection contract for a future Python Trigger.
    """

    endpoint: str = "https://api.stripe.com/v1"
    max_response_bytes: int = field(default=1_048_576, metadata={"dex_json_name": "maxResponseBytes"})
    webhook_max_body_bytes: int = field(default=1_048_576, metadata={"dex_json_name": "webhookMaxBodyBytes"})
    webhook_signature_tolerance: str | int = field(
        default="5m", metadata={"dex_json_name": "webhookSignatureTolerance"}
    )


@dataclass(frozen=True, slots=True)
class Credentials:
    """A Stripe secret key resolved outside durable Flow state."""

    secret_key: SecretString = field(repr=False)
    webhook_secret: SecretString | None = field(default=None, repr=False)
    auth_method_id: str = "api-key-only"


@dataclass(frozen=True, slots=True)
class CheckoutSession:
    """Safe Checkout Session summary, excluding payment method and bank details."""

    id: str = ""
    url: str = field(default="", metadata={"dex_json_omit_default": True})
    client_reference_id: str = field(
        default="", metadata={"dex_json_name": "clientReferenceId", "dex_json_omit_default": True}
    )
    payment_status: str = field(
        default="", metadata={"dex_json_name": "paymentStatus", "dex_json_omit_default": True}
    )
    status: str = field(default="", metadata={"dex_json_omit_default": True})
    payment_intent_id: str = field(
        default="", metadata={"dex_json_name": "paymentIntentId", "dex_json_omit_default": True}
    )
    currency: str = field(default="", metadata={"dex_json_omit_default": True})
    amount_total: int = field(
        default=0, metadata={"dex_json_name": "amountTotal", "dex_json_omit_default": True}
    )
    expires_at: datetime | None = field(
        default=None,
        metadata={"dex_json_name": "expiresAt", "dex_json_omit_default": True, "dex_json_format": "rfc3339"},
    )
    metadata: dict[str, str] = field(default_factory=dict, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class CreateACHCheckoutSessionInput:
    """Create a hosted US bank account Checkout Session; amounts use minor units."""

    client_reference_id: str = field(metadata={"dex_json_name": "clientReferenceId"})
    customer_email: str = field(metadata={"dex_json_name": "customerEmail"})
    success_url: str = field(metadata={"dex_json_name": "successUrl"})
    cancel_url: str = field(metadata={"dex_json_name": "cancelUrl"})
    currency: str
    unit_amount: int = field(metadata={"dex_json_name": "unitAmount"})
    product_name: str = field(metadata={"dex_json_name": "productName"})
    metadata: dict[str, str] = field(default_factory=dict, metadata={"dex_json_omit_default": True})
    expires_at: datetime | None = field(
        default=None,
        metadata={"dex_json_name": "expiresAt", "dex_json_omit_default": True, "dex_json_format": "rfc3339"},
    )


@dataclass(frozen=True, slots=True)
class CreateCardCheckoutSessionInput:
    """Create a hosted card Checkout Session; fields match the ACH request."""

    client_reference_id: str = field(metadata={"dex_json_name": "clientReferenceId"})
    customer_email: str = field(metadata={"dex_json_name": "customerEmail"})
    success_url: str = field(metadata={"dex_json_name": "successUrl"})
    cancel_url: str = field(metadata={"dex_json_name": "cancelUrl"})
    currency: str
    unit_amount: int = field(metadata={"dex_json_name": "unitAmount"})
    product_name: str = field(metadata={"dex_json_name": "productName"})
    metadata: dict[str, str] = field(default_factory=dict, metadata={"dex_json_omit_default": True})
    expires_at: datetime | None = field(
        default=None,
        metadata={"dex_json_name": "expiresAt", "dex_json_omit_default": True, "dex_json_format": "rfc3339"},
    )


@dataclass(frozen=True, slots=True)
class GetCheckoutSessionInput:
    """Identify one Checkout Session by its cs_ Stripe ID."""

    session_id: str = field(metadata={"dex_json_name": "sessionId"})


@dataclass(frozen=True, slots=True)
class GetPaymentIntentInput:
    """Identify one PaymentIntent by its pi_ Stripe ID."""

    payment_intent_id: str = field(metadata={"dex_json_name": "paymentIntentId"})


@dataclass(frozen=True, slots=True)
class PaymentIntent:
    """Safe PaymentIntent summary with latest-charge refunded amount in minor units."""

    id: str = ""
    amount: int = 0
    currency: str = ""
    status: str = ""
    amount_refunded: int = field(default=0, metadata={"dex_json_name": "amountRefunded"})
    customer_email: str = field(
        default="", metadata={"dex_json_name": "customerEmail", "dex_json_omit_default": True}
    )
    latest_charge_id: str = field(
        default="", metadata={"dex_json_name": "latestChargeId", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class CreateRefundInput:
    """Refund a PaymentIntent by a positive amount in minor currency units."""

    payment_intent_id: str = field(metadata={"dex_json_name": "paymentIntentId"})
    amount: int
    reason: str = field(default="", metadata={"dex_json_omit_default": True})
    metadata: dict[str, str] = field(default_factory=dict, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class Refund:
    """Safe Stripe refund summary; accepted refunds can remain pending."""

    refund_id: str = field(default="", metadata={"dex_json_name": "refundId"})
    status: str = ""
    amount: int = 0
    currency: str = ""
