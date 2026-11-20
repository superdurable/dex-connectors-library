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


# ListWarehousesInput selects the authorized customer's entire warehouse list.
@dataclass(frozen=True, slots=True)
class ListWarehousesInput:
    pass


setattr(ListWarehousesInput, '__module__', 'dex_connectors.yanwen.models')


# Warehouse identifies a Yanwen handover warehouse.
@dataclass(frozen=True, slots=True)
class Warehouse:
    # Code is the provider's stable warehouse code.
    code: str = ''
    # Name is the provider's warehouse name.
    name: str = ''
    # Area is the provider's warehouse region.
    area: str = ''


setattr(Warehouse, '__module__', 'dex_connectors.yanwen.models')


# WarehouseList is a bounded list of authorized handover warehouses.
@dataclass(frozen=True, slots=True)
class WarehouseList:
    # Warehouses contains at most 500 warehouses.
    warehouses: tuple[Warehouse, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'warehouses', _replace_nil_slices(self.warehouses, 1))


WarehouseList.__annotations__['warehouses'] = tuple[Warehouse, ...] | None
WarehouseList.__dataclass_fields__['warehouses'].type = tuple[Warehouse, ...] | None
setattr(WarehouseList, '__module__', 'dex_connectors.yanwen.models')
