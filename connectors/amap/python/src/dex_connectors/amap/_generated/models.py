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


# GeocodeAddressInput selects a structured address and optional city scope.
@dataclass(frozen=True, slots=True)
class GeocodeAddressInput:
    # Address is the Chinese address, from province to street number.
    address: str = ''
    # City optionally limits matching to a Chinese city name, pinyin, citycode, or adcode.
    city: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(GeocodeAddressInput, '__module__', 'dex_connectors.amap.models')


# GeocodedLocation is one location candidate returned by Amap.
@dataclass(frozen=True, slots=True)
class GeocodedLocation:
    # FormattedAddress is Amap's complete address.
    formatted_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'formattedAddress'})
    # Province is the province or direct-administered municipality.
    province: str = ''
    # City is the city when Amap supplies one.
    city: str = ''
    # District is the district when Amap supplies one.
    district: str = ''
    # Adcode is Amap's administrative code.
    adcode: str = ''
    # Longitude is the first coordinate in Amap's location pair.
    longitude: float = 0.0
    # Latitude is the second coordinate in Amap's location pair.
    latitude: float = 0.0
    # Level is Amap's match-precision label.
    level: str = ''


setattr(GeocodedLocation, '__module__', 'dex_connectors.amap.models')


# GeocodeAddressOutput contains zero or more provider location candidates.
@dataclass(frozen=True, slots=True)
class GeocodeAddressOutput:
    # Locations is empty when Amap found no match.
    locations: tuple[GeocodedLocation, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'locations', _replace_nil_slices(self.locations, 1))


GeocodeAddressOutput.__annotations__['locations'] = tuple[GeocodedLocation, ...] | None
GeocodeAddressOutput.__dataclass_fields__['locations'].type = tuple[GeocodedLocation, ...] | None
setattr(GeocodeAddressOutput, '__module__', 'dex_connectors.amap.models')
