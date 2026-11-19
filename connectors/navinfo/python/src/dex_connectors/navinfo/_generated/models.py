# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# LookupIPLocationInput identifies one IPv4 address.
@dataclass(frozen=True, slots=True)
class LookupIPLocationInput:
    # IPAddress is a literal address, not a hostname or CIDR range.
    ip_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'ipAddress'})


setattr(LookupIPLocationInput, '__module__', 'dex_connectors.navinfo.models')


# LookupIPLocationOutput contains only coarse NavInfo location fields.
@dataclass(frozen=True, slots=True)
class LookupIPLocationOutput:
    # Province is the province or municipality name.
    province: str = ''
    # City is the city name.
    city: str = ''
    # Adcode is the city administrative code.
    adcode: str = ''


setattr(LookupIPLocationOutput, '__module__', 'dex_connectors.navinfo.models')
