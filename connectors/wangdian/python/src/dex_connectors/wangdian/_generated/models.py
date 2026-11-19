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


# ListShopsInput selects a zero-based shop page. Zero page size uses 30.
@dataclass(frozen=True, slots=True)
class ListShopsInput:
    # Page is the zero-based page number.
    page: int = 0
    # PageSize is the number of shops per page, from 1 to 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListShopsInput, '__module__', 'dex_connectors.wangdian.models')


# ShopSummary excludes contacts, account identifiers, and provider tokens.
@dataclass(frozen=True, slots=True)
class ShopSummary:
    # ID is the provider's shop ID, preserved as text for JSON precision.
    id: str = ''
    # Number is the shop's ERP code.
    number: str = ''
    # Name is the ERP shop name.
    name: str = ''
    # PlatformID identifies the shop platform.
    platform_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'platformID'})
    # AuthorizationState is 0 unapproved, 1 approved, 2 expired, or 3 disabled.
    authorization_state: int = _dataclass_field(default=0, metadata={'dex_json_name': 'authorizationState'})


setattr(ShopSummary, '__module__', 'dex_connectors.wangdian.models')


# ListShopsOutput contains a bounded shop page and next page number.
@dataclass(frozen=True, slots=True)
class ListShopsOutput:
    # Shops are returned in provider order.
    shops: tuple[ShopSummary, ...] = ()
    # Total is the provider-reported number of enabled shops.
    total: int = 0
    # NextPage is the next zero-based page, or zero when terminal.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'shops', _replace_nil_slices(self.shops, 1))


ListShopsOutput.__annotations__['shops'] = tuple[ShopSummary, ...] | None
ListShopsOutput.__dataclass_fields__['shops'].type = tuple[ShopSummary, ...] | None
setattr(ListShopsOutput, '__module__', 'dex_connectors.wangdian.models')
