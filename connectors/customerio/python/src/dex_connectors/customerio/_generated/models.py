# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# GetCustomersByEmailInput identifies an exact non-secret customer email address.
@dataclass(frozen=True, slots=True)
class GetCustomersByEmailInput:
    # Email is the complete email address shown on the Customer.io People page.
    email: str = ''


setattr(GetCustomersByEmailInput, '__module__', 'dex_connectors.customerio.models')


# CustomerLookupPage contains Customer.io profile IDs matching an email.
@dataclass(frozen=True, slots=True)
class CustomerLookupPage:
    # Email is the address supplied to Customer.io.
    email: str = ''
    # CustomerIDs are the matching cio_id values in provider order.
    customer_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'customerIDs'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'customer_ids', _replace_nil_slices(self.customer_ids, 1))


CustomerLookupPage.__annotations__['customer_ids'] = tuple[str, ...] | None
CustomerLookupPage.__dataclass_fields__['customer_ids'].type = tuple[str, ...] | None
setattr(CustomerLookupPage, '__module__', 'dex_connectors.customerio.models')
