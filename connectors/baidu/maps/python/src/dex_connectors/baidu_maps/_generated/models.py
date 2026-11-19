# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GeocodeAddressInput selects a structured Chinese address and optional city.
@dataclass(frozen=True, slots=True)
class GeocodeAddressInput:
    # Address is a UTF-8 address of at most 128 bytes, from province to street number.
    address: str = ''
    # City is an optional Chinese city name used to help distinguish repeated addresses.
    city: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(GeocodeAddressInput, '__module__', 'dex_connectors.baidu_maps.models')


# GeocodeAddressOutput is Baidu Maps' best matching BD-09 location and precision measures.
@dataclass(frozen=True, slots=True)
class GeocodeAddressOutput:
    # Longitude is the BD-09 east-west coordinate in degrees.
    longitude: float = 0.0
    # Latitude is the BD-09 north-south coordinate in degrees.
    latitude: float = 0.0
    # Precise reports whether Baidu Maps found an exact point.
    precise: bool = False
    # Confidence is the provider's 0 through 100 absolute precision score.
    confidence: int = 0
    # Comprehension is the provider's 0 through 100 address understanding score.
    comprehension: int = 0
    # Level is the provider's match structure, such as a road or door address.
    level: str = ''


setattr(GeocodeAddressOutput, '__module__', 'dex_connectors.baidu_maps.models')
