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


# GetRegionInput has no operation parameters; UCloud scopes the response to the API key.
@dataclass(frozen=True, slots=True)
class GetRegionInput:
    pass


setattr(GetRegionInput, '__module__', 'dex_connectors.ucloud.models')


# RegionInfo identifies one UCloud region and availability zone accessible to the account.
@dataclass(frozen=True, slots=True)
class RegionInfo:
    # RegionID is the provider's data-center identifier.
    region_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'regionId'})
    # RegionName is the data-center display name.
    region_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'regionName'})
    # IsDefault reports the account's current default data center.
    is_default: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDefault'})
    # BitMaps contains the provider's permission bits for this data center.
    bit_maps: str = _dataclass_field(default='', metadata={'dex_json_name': 'bitMaps'})
    # Region is the provider region code, such as cn-bj.
    region: str = ''
    # Zone is the provider availability-zone code, such as cn-bj-01.
    zone: str = ''


setattr(RegionInfo, '__module__', 'dex_connectors.ucloud.models')


# GetRegionOutput contains accessible region and zone entries.
@dataclass(frozen=True, slots=True)
class GetRegionOutput:
    # Regions is empty when UCloud returns no entries.
    regions: tuple[RegionInfo, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'regions', _replace_nil_slices(self.regions, 1))


GetRegionOutput.__annotations__['regions'] = tuple[RegionInfo, ...] | None
GetRegionOutput.__dataclass_fields__['regions'].type = tuple[RegionInfo, ...] | None
setattr(GetRegionOutput, '__module__', 'dex_connectors.ucloud.models')
