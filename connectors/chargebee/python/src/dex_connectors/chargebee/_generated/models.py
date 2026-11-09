# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetSubscriptionInput selects a subscription by its non-secret Chargebee ID.
@dataclass(frozen=True, slots=True)
class GetSubscriptionInput:
    # SubscriptionID is the ID shown in Chargebee Subscriptions or returned by the API.
    subscription_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'subscriptionId'})


setattr(GetSubscriptionInput, '__module__', 'dex_connectors.chargebee.models')


# SubscriptionStatus excludes customer and payment details.
@dataclass(frozen=True, slots=True)
class SubscriptionStatus:
    # ID is the Chargebee subscription ID.
    id: str = ''
    # CurrencyCode is the subscription's ISO 4217 currency.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode'})
    # Status is the subscription state such as active.
    status: str = ''


setattr(SubscriptionStatus, '__module__', 'dex_connectors.chargebee.models')
