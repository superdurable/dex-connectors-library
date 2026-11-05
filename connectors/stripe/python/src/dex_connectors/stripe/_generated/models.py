# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime

from .timestamps import parse_go_datetime, validate_go_datetime

# CreateCardCheckoutSessionInput is one hosted card Checkout request.
# Its fields have the same meanings and validation as CreateACHCheckoutSessionInput.
@dataclass(frozen=True, slots=True)
class CreateCardCheckoutSessionInput:
    # ClientReferenceID correlates the session with one non-sensitive application record.
    client_reference_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'clientReferenceId'})
    # CustomerEmail is the plain email address Stripe pre-fills in hosted Checkout.
    customer_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerEmail'})
    # SuccessURL is the HTTPS location Stripe uses after Checkout succeeds.
    success_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'successUrl'})
    # CancelURL is the HTTPS location Stripe uses when the customer cancels Checkout.
    cancel_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'cancelUrl'})
    # Currency is the lowercase three-letter currency code.
    currency: str = ''
    # UnitAmount is the positive ticket price in the currency's minor unit.
    unit_amount: int = _dataclass_field(default=0, metadata={'dex_json_name': 'unitAmount'})
    # ProductName is the ticket or event label displayed by hosted Checkout.
    product_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'productName'})
    # Metadata is bounded non-sensitive application metadata copied to the session and PaymentIntent.
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # ExpiresAt optionally requests a provider expiration timestamp.
    expires_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'expiresAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.expires_at, str):
            object.__setattr__(self, 'expires_at', parse_go_datetime(self.expires_at, 'expires_at'))
        elif self.expires_at is not None:
            validate_go_datetime(self.expires_at, 'expires_at')


setattr(CreateCardCheckoutSessionInput, '__module__', 'dex_connectors.stripe.models')


# CheckoutSession is the connector-safe subset of a Stripe Checkout Session.
@dataclass(frozen=True, slots=True)
class CheckoutSession:
    # ID is Stripe's Checkout Session identifier.
    id: str = ''
    # URL is Stripe's hosted Checkout URL while the session is open.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ClientReferenceID is the non-sensitive application reference supplied at creation.
    client_reference_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'clientReferenceId', 'dex_json_omit_default': True})
    # PaymentStatus is Stripe's payment status, such as unpaid or paid.
    payment_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentStatus', 'dex_json_omit_default': True})
    # Status is Stripe's Checkout Session status, such as open, complete, or expired.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PaymentIntentID is the safe Stripe PaymentIntent correlation identifier when present.
    payment_intent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentIntentId', 'dex_json_omit_default': True})
    # Currency is the lowercase three-letter settlement currency.
    currency: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # AmountTotal is the total amount in the currency's minor unit.
    amount_total: int = _dataclass_field(default=0, metadata={'dex_json_name': 'amountTotal', 'dex_json_omit_default': True})
    # ExpiresAt is the Checkout Session expiration time when present.
    expires_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'expiresAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # Metadata contains the bounded non-sensitive application metadata supplied at creation.
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.expires_at, str):
            object.__setattr__(self, 'expires_at', parse_go_datetime(self.expires_at, 'expires_at'))
        elif self.expires_at is not None:
            validate_go_datetime(self.expires_at, 'expires_at')


setattr(CheckoutSession, '__module__', 'dex_connectors.stripe.models')


# CreateACHCheckoutSessionInput contains one Stripe-hosted ACH Checkout request.
@dataclass(frozen=True, slots=True)
class CreateACHCheckoutSessionInput:
    # ClientReferenceID correlates the session with one non-sensitive application record.
    client_reference_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'clientReferenceId'})
    # CustomerEmail is the plain email address Stripe pre-fills in hosted Checkout.
    customer_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerEmail'})
    # SuccessURL is the HTTPS location Stripe uses after Checkout succeeds.
    success_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'successUrl'})
    # CancelURL is the HTTPS location Stripe uses when the customer cancels Checkout.
    cancel_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'cancelUrl'})
    # Currency is the lowercase three-letter currency code.
    currency: str = ''
    # UnitAmount is the positive ticket price in the currency's minor unit.
    unit_amount: int = _dataclass_field(default=0, metadata={'dex_json_name': 'unitAmount'})
    # ProductName is the ticket or event label displayed by hosted Checkout.
    product_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'productName'})
    # Metadata is bounded non-sensitive application metadata copied to the session and PaymentIntent.
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # ExpiresAt optionally requests a provider expiration timestamp.
    expires_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'expiresAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.expires_at, str):
            object.__setattr__(self, 'expires_at', parse_go_datetime(self.expires_at, 'expires_at'))
        elif self.expires_at is not None:
            validate_go_datetime(self.expires_at, 'expires_at')


setattr(CreateACHCheckoutSessionInput, '__module__', 'dex_connectors.stripe.models')


# GetCheckoutSessionInput identifies one Stripe Checkout Session.
@dataclass(frozen=True, slots=True)
class GetCheckoutSessionInput:
    # SessionID is Stripe's Checkout Session identifier.
    session_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'sessionId'})


setattr(GetCheckoutSessionInput, '__module__', 'dex_connectors.stripe.models')


# GetPaymentIntentInput identifies one Stripe PaymentIntent.
@dataclass(frozen=True, slots=True)
class GetPaymentIntentInput:
    # PaymentIntentID is Stripe's PaymentIntent identifier, beginning with pi_.
    payment_intent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentIntentId'})


setattr(GetPaymentIntentInput, '__module__', 'dex_connectors.stripe.models')


# PaymentIntent is the connector-safe subset of a Stripe PaymentIntent and its latest charge.
@dataclass(frozen=True, slots=True)
class PaymentIntent:
    # ID is Stripe's PaymentIntent identifier.
    id: str = ''
    # Amount is the intended payment amount in the currency's minor unit.
    amount: int = 0
    # Currency is the lowercase three-letter currency code.
    currency: str = ''
    # Status is Stripe's PaymentIntent status, such as requires_payment_method or succeeded.
    status: str = ''
    # AmountRefunded is the latest charge's refunded amount in minor units; zero without a charge.
    amount_refunded: int = _dataclass_field(default=0, metadata={'dex_json_name': 'amountRefunded'})
    # CustomerEmail is the receipt email, or the latest charge's billing email when no receipt email is set.
    customer_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerEmail', 'dex_json_omit_default': True})
    # LatestChargeID is Stripe's identifier of the latest charge when one exists.
    latest_charge_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'latestChargeId', 'dex_json_omit_default': True})


setattr(PaymentIntent, '__module__', 'dex_connectors.stripe.models')


# CreateRefundInput contains one Stripe refund request.
@dataclass(frozen=True, slots=True)
class CreateRefundInput:
    # PaymentIntentID is the Stripe PaymentIntent to refund, beginning with pi_.
    payment_intent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentIntentId'})
    # Amount is the positive refund amount in the currency's minor unit.
    amount: int = 0
    # Reason optionally classifies the refund as duplicate, fraudulent, or requested_by_customer.
    reason: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Metadata is bounded non-sensitive application metadata stored on the refund.
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(CreateRefundInput, '__module__', 'dex_connectors.stripe.models')


# Refund is the connector-safe subset of a Stripe refund.
@dataclass(frozen=True, slots=True)
class Refund:
    # RefundID is Stripe's refund identifier, beginning with re_.
    refund_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'refundId'})
    # Status is succeeded, pending, failed, requires_action, or canceled.
    status: str = ''
    # Amount is the refunded amount in the currency's minor unit.
    amount: int = 0
    # Currency is the lowercase three-letter currency code.
    currency: str = ''


setattr(Refund, '__module__', 'dex_connectors.stripe.models')
