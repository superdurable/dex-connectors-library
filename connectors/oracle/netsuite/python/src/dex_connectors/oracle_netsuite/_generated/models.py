# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCustomerInput identifies a customer by its internal NetSuite ID.
@dataclass(frozen=True, slots=True)
class GetCustomerInput:
    # CustomerID is the non-secret numeric internal ID shown on the customer record.
    customer_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerId'})


setattr(GetCustomerInput, '__module__', 'dex_connectors.oracle_netsuite.models')


# CustomerSummary contains the customer's internal and entity IDs.
@dataclass(frozen=True, slots=True)
class CustomerSummary:
    # ID is the customer's internal ID.
    id: str = ''
    # EntityID is the customer ID displayed by NetSuite.
    entity_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'entityId'})


setattr(CustomerSummary, '__module__', 'dex_connectors.oracle_netsuite.models')
