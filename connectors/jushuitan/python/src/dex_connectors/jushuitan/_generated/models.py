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


# ListShopsInput selects one page of enabled shops.
@dataclass(frozen=True, slots=True)
class ListShopsInput:
    # PageIndex starts at one.
    page_index: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageIndex'})
    # PageSize is the number of shops to request, from one through one hundred.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListShopsInput, '__module__', 'dex_connectors.jushuitan.models')


# Shop contains non-secret shop metadata.
@dataclass(frozen=True, slots=True)
class Shop:
    # ShopID identifies the ERP shop.
    shop_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'shopId'})
    # Name is the shop's displayed name.
    name: str = ''
    # Site is the platform name reported by Jushuitan.
    site: str = ''


setattr(Shop, '__module__', 'dex_connectors.jushuitan.models')


# ListShopsOutput contains one page of enabled ERP shops.
@dataclass(frozen=True, slots=True)
class ListShopsOutput:
    # PageIndex is the provider's one-based page number.
    page_index: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageIndex'})
    # PageSize is the provider's page size.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})
    # HasNext indicates another page is available.
    has_next: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasNext'})
    # Shops contains the shops in this page.
    shops: tuple[Shop, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'shops', _replace_nil_slices(self.shops, 1))


ListShopsOutput.__annotations__['shops'] = tuple[Shop, ...] | None
ListShopsOutput.__dataclass_fields__['shops'].type = tuple[Shop, ...] | None
setattr(ListShopsOutput, '__module__', 'dex_connectors.jushuitan.models')
