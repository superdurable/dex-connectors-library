# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# LookupIPLocationInput identifies one IPv4 or IPv6 address.
@dataclass(frozen=True, slots=True)
class LookupIPLocationInput:
    # IPAddress is a valid IPv4 or IPv6 address, not a hostname or CIDR range.
    ip_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'ipAddress'})


setattr(LookupIPLocationInput, '__module__', 'dex_connectors.ipip.models')


# LookupIPLocationOutput contains only the API's default coarse location fields.
@dataclass(frozen=True, slots=True)
class LookupIPLocationOutput:
    # CountryName is the location country name.
    country_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'countryName'})
    # RegionName is the province or state name.
    region_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'regionName'})
    # CityName is the city name.
    city_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'cityName'})
    # CountryCode is the two-letter country code.
    country_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'countryCode'})


setattr(LookupIPLocationOutput, '__module__', 'dex_connectors.ipip.models')
