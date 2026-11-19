# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListWarehousesInput selects the tenant's authorized warehouse catalog.
@dataclass(frozen=True, slots=True)
class ListWarehousesInput:
    pass


setattr(ListWarehousesInput, '__module__', 'dex_connectors.yijingtong.models')


# Warehouse identifies a Yijingtong warehouse and its country.
@dataclass(frozen=True, slots=True)
class Warehouse:
    # ID is the provider's warehouse code.
    id: str = ''
    # Name is the provider's warehouse display name.
    name: str = ''
    # Country is the provider's country display name.
    country: str = ''


setattr(Warehouse, '__module__', 'dex_connectors.yijingtong.models')


# WarehousePage contains the bounded authorized warehouse catalog.
@dataclass(frozen=True, slots=True)
class WarehousePage:
    # Warehouses contains at most 200 records.
    warehouses: tuple[Warehouse, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'warehouses', _replace_nil_slices(self.warehouses, 1))


WarehousePage.__annotations__['warehouses'] = tuple[Warehouse, ...] | None
WarehousePage.__dataclass_fields__['warehouses'].type = tuple[Warehouse, ...] | None
setattr(WarehousePage, '__module__', 'dex_connectors.yijingtong.models')
