# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime

@dataclass(frozen=True, slots=True)
class CreateCardCheckoutSessionInput:
    """Typed CreateCardCheckoutSessionInput value from the Go connector contract."""

    client_reference_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'clientReferenceId'})
    customer_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerEmail'})
    success_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'successUrl'})
    cancel_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'cancelUrl'})
    currency: str = ''
    unit_amount: int = _dataclass_field(default=0, metadata={'dex_json_name': 'unitAmount'})
    product_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'productName'})
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    expires_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'expiresAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(CreateCardCheckoutSessionInput, '__module__', 'dex_connectors.stripe.models')


@dataclass(frozen=True, slots=True)
class CheckoutSession:
    """Typed CheckoutSession value from the Go connector contract."""

    id: str = ''
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    client_reference_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'clientReferenceId', 'dex_json_omit_default': True})
    payment_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentStatus', 'dex_json_omit_default': True})
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    payment_intent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentIntentId', 'dex_json_omit_default': True})
    currency: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    amount_total: int = _dataclass_field(default=0, metadata={'dex_json_name': 'amountTotal', 'dex_json_omit_default': True})
    expires_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'expiresAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(CheckoutSession, '__module__', 'dex_connectors.stripe.models')


@dataclass(frozen=True, slots=True)
class CreateACHCheckoutSessionInput:
    """Typed CreateACHCheckoutSessionInput value from the Go connector contract."""

    client_reference_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'clientReferenceId'})
    customer_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerEmail'})
    success_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'successUrl'})
    cancel_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'cancelUrl'})
    currency: str = ''
    unit_amount: int = _dataclass_field(default=0, metadata={'dex_json_name': 'unitAmount'})
    product_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'productName'})
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    expires_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'expiresAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(CreateACHCheckoutSessionInput, '__module__', 'dex_connectors.stripe.models')


@dataclass(frozen=True, slots=True)
class GetCheckoutSessionInput:
    """Typed GetCheckoutSessionInput value from the Go connector contract."""

    session_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'sessionId'})


setattr(GetCheckoutSessionInput, '__module__', 'dex_connectors.stripe.models')


@dataclass(frozen=True, slots=True)
class GetPaymentIntentInput:
    """Typed GetPaymentIntentInput value from the Go connector contract."""

    payment_intent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentIntentId'})


setattr(GetPaymentIntentInput, '__module__', 'dex_connectors.stripe.models')


@dataclass(frozen=True, slots=True)
class PaymentIntent:
    """Typed PaymentIntent value from the Go connector contract."""

    id: str = ''
    amount: int = 0
    currency: str = ''
    status: str = ''
    amount_refunded: int = _dataclass_field(default=0, metadata={'dex_json_name': 'amountRefunded'})
    customer_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerEmail', 'dex_json_omit_default': True})
    latest_charge_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'latestChargeId', 'dex_json_omit_default': True})


setattr(PaymentIntent, '__module__', 'dex_connectors.stripe.models')


@dataclass(frozen=True, slots=True)
class CreateRefundInput:
    """Typed CreateRefundInput value from the Go connector contract."""

    payment_intent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentIntentId'})
    amount: int = 0
    reason: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(CreateRefundInput, '__module__', 'dex_connectors.stripe.models')


@dataclass(frozen=True, slots=True)
class Refund:
    """Typed Refund value from the Go connector contract."""

    refund_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'refundId'})
    status: str = ''
    amount: int = 0
    currency: str = ''


setattr(Refund, '__module__', 'dex_connectors.stripe.models')
