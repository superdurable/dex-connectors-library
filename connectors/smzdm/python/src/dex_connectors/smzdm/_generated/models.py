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


# SearchProductsInput selects a bounded first page from the SMZDM product index.
@dataclass(frozen=True, slots=True)
class SearchProductsInput:
    # Keyword is the product search term, limited to 100 Unicode characters.
    keyword: str = ''


setattr(SearchProductsInput, '__module__', 'dex_connectors.smzdm.models')


# Product contains the public title and provider-reported price of a product-index entry.
@dataclass(frozen=True, slots=True)
class Product:
    # Title is the indexed product title.
    title: str = ''
    # PriceYuan is the provider-reported product price in Chinese yuan.
    price_yuan: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'priceYuan'})


setattr(Product, '__module__', 'dex_connectors.smzdm.models')


# ProductPage is the first page of matching product-index entries.
@dataclass(frozen=True, slots=True)
class ProductPage:
    # Total is the provider-reported matching entry count.
    total: int = 0
    # Products contains at most 20 items from the first page.
    products: tuple[Product, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'products', _replace_nil_slices(self.products, 1))


ProductPage.__annotations__['products'] = tuple[Product, ...] | None
ProductPage.__dataclass_fields__['products'].type = tuple[Product, ...] | None
setattr(ProductPage, '__module__', 'dex_connectors.smzdm.models')
