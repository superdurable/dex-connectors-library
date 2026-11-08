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


# ListAccessibleCustomersInput bounds how many account names enter Flow state.
@dataclass(frozen=True, slots=True)
class ListAccessibleCustomersInput:
    # MaxCustomers accepts 1 through 10000 names; zero uses 1000.
    max_customers: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxCustomers', 'dex_json_omit_default': True})


setattr(ListAccessibleCustomersInput, '__module__', 'dex_connectors.google_ads.models')


# AccessibleCustomers contains the customer resource names returned by Google Ads.
@dataclass(frozen=True, slots=True)
class AccessibleCustomers:
    # ResourceNames contain IDs in the form customers/{customer-id}.
    resource_names: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'resourceNames'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'resource_names', _replace_nil_slices(self.resource_names, 1))


AccessibleCustomers.__annotations__['resource_names'] = tuple[str, ...] | None
AccessibleCustomers.__dataclass_fields__['resource_names'].type = tuple[str, ...] | None
setattr(AccessibleCustomers, '__module__', 'dex_connectors.google_ads.models')
