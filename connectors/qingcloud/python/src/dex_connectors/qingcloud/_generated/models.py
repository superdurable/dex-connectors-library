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


# DescribeZonesInput has no parameters; QingCloud lists accessible zones.
@dataclass(frozen=True, slots=True)
class DescribeZonesInput:
    pass


setattr(DescribeZonesInput, '__module__', 'dex_connectors.qingcloud.models')


# Zone describes one accessible IaaS region.
@dataclass(frozen=True, slots=True)
class Zone:
    # ZoneID is the QingCloud zone identifier.
    zone_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'zoneId'})
    # Status is active, faulty, or defunct.
    status: str = ''


setattr(Zone, '__module__', 'dex_connectors.qingcloud.models')


# ZonesPage contains at most 100 accessible zones and the provider's total count.
@dataclass(frozen=True, slots=True)
class ZonesPage:
    # Zones is the page returned by QingCloud.
    zones: tuple[Zone, ...] = ()
    # TotalCount is the number of zones matching the query.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'zones', _replace_nil_slices(self.zones, 1))


ZonesPage.__annotations__['zones'] = tuple[Zone, ...] | None
ZonesPage.__dataclass_fields__['zones'].type = tuple[Zone, ...] | None
setattr(ZonesPage, '__module__', 'dex_connectors.qingcloud.models')
