# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCustomerInput selects a customer inside one Wave business.
@dataclass(frozen=True, slots=True)
class GetCustomerInput:
    # BusinessID is the exact GraphQL ID of the Wave business.
    business_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'businessId'})
    # CustomerID is the exact GraphQL ID of the customer.
    customer_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerId'})


setattr(GetCustomerInput, '__module__', 'dex_connectors.wave.models')


# CustomerProfile contains the selected customer ID and name.
@dataclass(frozen=True, slots=True)
class CustomerProfile:
    # BusinessID is the business used for the query.
    business_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'businessId'})
    # ID is the customer GraphQL ID returned by Wave.
    id: str = ''
    # Name is the customer's display name.
    name: str = ''


setattr(CustomerProfile, '__module__', 'dex_connectors.wave.models')
