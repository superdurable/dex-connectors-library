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


# ListRootGoodsCategoriesInput has no arguments; the operation requests parent_id zero.
@dataclass(frozen=True, slots=True)
class ListRootGoodsCategoriesInput:
    pass


setattr(ListRootGoodsCategoriesInput, '__module__', 'dex_connectors.xiaoetech.models')


# GoodsCategory is a root category without nested or private provider metadata.
@dataclass(frozen=True, slots=True)
class GoodsCategory:
    # ID is the provider's category identifier.
    id: int = 0
    # Title is the provider's category title.
    title: str = ''
    # HasChildren reports whether child categories exist.
    has_children: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasChildren'})


setattr(GoodsCategory, '__module__', 'dex_connectors.xiaoetech.models')


# ListRootGoodsCategoriesOutput holds the store's root goods categories.
@dataclass(frozen=True, slots=True)
class ListRootGoodsCategoriesOutput:
    # Categories contains only top-level goods categories.
    categories: tuple[GoodsCategory, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'categories', _replace_nil_slices(self.categories, 1))


ListRootGoodsCategoriesOutput.__annotations__['categories'] = tuple[GoodsCategory, ...] | None
ListRootGoodsCategoriesOutput.__dataclass_fields__['categories'].type = tuple[GoodsCategory, ...] | None
setattr(ListRootGoodsCategoriesOutput, '__module__', 'dex_connectors.xiaoetech.models')
