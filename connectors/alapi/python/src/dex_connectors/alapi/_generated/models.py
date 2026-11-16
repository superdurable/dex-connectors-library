# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# LookupIPLocationInput identifies one public IPv4 address.
@dataclass(frozen=True, slots=True)
class LookupIPLocationInput:
    # IPAddress is an IPv4 address, not a hostname or CIDR range.
    ip_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'ipAddress'})


setattr(LookupIPLocationInput, '__module__', 'dex_connectors.alapi.models')


# LookupIPLocationOutput contains only coarse location and network fields.
@dataclass(frozen=True, slots=True)
class LookupIPLocationOutput:
    # Country is the country name when returned by ALAPI.
    country: str = ''
    # Province is the province name.
    province: str = ''
    # City is the city name.
    city: str = ''
    # District is the district name.
    district: str = ''
    # Operator is the network operator name.
    operator: str = ''


setattr(LookupIPLocationOutput, '__module__', 'dex_connectors.alapi.models')
