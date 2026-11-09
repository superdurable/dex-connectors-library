# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetPaymentLinkInput selects a payment link by its non-secret Adyen ID.
@dataclass(frozen=True, slots=True)
class GetPaymentLinkInput:
    # PaymentLinkID is a payment link ID returned by Adyen Checkout API.
    payment_link_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentLinkId'})


setattr(GetPaymentLinkInput, '__module__', 'dex_connectors.adyen.models')


# PaymentLinkStatus excludes shopper identities and payment details.
@dataclass(frozen=True, slots=True)
class PaymentLinkStatus:
    # ID is the Adyen payment link ID.
    id: str = ''
    # Status is a payment link state such as completed.
    status: str = ''
    # CurrencyCode is the link amount's ISO 4217 currency.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode'})


setattr(PaymentLinkStatus, '__module__', 'dex_connectors.adyen.models')
