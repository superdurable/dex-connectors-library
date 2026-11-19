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


# ListProductsInput has no selector; ZenTao REST v1 lists visible products.
@dataclass(frozen=True, slots=True)
class ListProductsInput:
    pass


setattr(ListProductsInput, '__module__', 'dex_connectors.zentao.models')


# Product is a ZenTao product without any personnel fields.
@dataclass(frozen=True, slots=True)
class Product:
    # ID is the product's integer identifier.
    id: int = 0
    # Name is the product display name.
    name: str = ''
    # Code is the product's code, which may be empty.
    code: str = ''
    # Type is normal, branch, or platform.
    type: str = ''
    # AccessControl is open or private.
    access_control: str = _dataclass_field(default='', metadata={'dex_json_name': 'accessControl'})


setattr(Product, '__module__', 'dex_connectors.zentao.models')


# ProductList contains the visible products and total count.
@dataclass(frozen=True, slots=True)
class ProductList:
    # Total is ZenTao's reported product count.
    total: int = 0
    # Products contains only product metadata and no user data.
    products: tuple[Product, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'products', _replace_nil_slices(self.products, 1))


ProductList.__annotations__['products'] = tuple[Product, ...] | None
ProductList.__dataclass_fields__['products'].type = tuple[Product, ...] | None
setattr(ProductList, '__module__', 'dex_connectors.zentao.models')
