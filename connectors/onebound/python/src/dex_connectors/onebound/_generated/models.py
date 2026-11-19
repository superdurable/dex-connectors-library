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


# SearchProductsInput selects public 1688 listings by a keyword and one-based page.
@dataclass(frozen=True, slots=True)
class SearchProductsInput:
    # Keyword is a nonempty public product search term, at most 100 runes.
    keyword: str = ''
    # PageNumber is one-based; zero selects the first page.
    page_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNumber'})


setattr(SearchProductsInput, '__module__', 'dex_connectors.onebound.models')


# ProductSearchItem contains a public listing's product metadata.
@dataclass(frozen=True, slots=True)
class ProductSearchItem:
    # ProductID is 1688's product identifier.
    product_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'productID'})
    # Title is the public listing title.
    title: str = ''
    # Price is the provider's displayed price string.
    price: str = ''
    # DetailURL is the public 1688 product page.
    detail_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'detailURL'})


setattr(ProductSearchItem, '__module__', 'dex_connectors.onebound.models')


# ProductSearchPage contains at most 40 public listings and pagination metadata.
@dataclass(frozen=True, slots=True)
class ProductSearchPage:
    # PageNumber is the provider's one-based page number.
    page_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNumber'})
    # Total is the provider's total matching product count.
    total: int = 0
    # Products contains at most 40 product listings.
    products: tuple[ProductSearchItem, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'products', _replace_nil_slices(self.products, 1))


ProductSearchPage.__annotations__['products'] = tuple[ProductSearchItem, ...] | None
ProductSearchPage.__dataclass_fields__['products'].type = tuple[ProductSearchItem, ...] | None
setattr(ProductSearchPage, '__module__', 'dex_connectors.onebound.models')
