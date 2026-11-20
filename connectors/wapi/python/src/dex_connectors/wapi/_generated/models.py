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


# ListVehicleBrandsInput requests the public brand-price list for an authorized app.
@dataclass(frozen=True, slots=True)
class ListVehicleBrandsInput:
    pass


setattr(ListVehicleBrandsInput, '__module__', 'dex_connectors.wapi.models')


# VehicleBrandPrice is one provider brand and maintenance-record query price.
@dataclass(frozen=True, slots=True)
class VehicleBrandPrice:
    # ID is the Wapi brand identifier.
    id: str = ''
    # Name is the display name of the brand.
    name: str = ''
    # PriceFen is the quoted query price in Chinese fen.
    price_fen: int = _dataclass_field(default=0, metadata={'dex_json_name': 'priceFen'})


setattr(VehicleBrandPrice, '__module__', 'dex_connectors.wapi.models')


# ListVehicleBrandsOutput contains the provider count and at most 200 brands.
@dataclass(frozen=True, slots=True)
class ListVehicleBrandsOutput:
    # Total is the number of brands returned by the provider.
    total: int = 0
    # Brands are the first 200 brand prices.
    brands: tuple[VehicleBrandPrice, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'brands', _replace_nil_slices(self.brands, 1))


ListVehicleBrandsOutput.__annotations__['brands'] = tuple[VehicleBrandPrice, ...] | None
ListVehicleBrandsOutput.__dataclass_fields__['brands'].type = tuple[VehicleBrandPrice, ...] | None
setattr(ListVehicleBrandsOutput, '__module__', 'dex_connectors.wapi.models')
