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


# DescribeRegionsInput has no business parameters; the connector lists available ECS regions.
@dataclass(frozen=True, slots=True)
class DescribeRegionsInput:
    pass


setattr(DescribeRegionsInput, '__module__', 'dex_connectors.alibaba.models')


# ECSRegion is a region advertised by ECS.
@dataclass(frozen=True, slots=True)
class ECSRegion:
    # RegionID is the canonical ECS region ID.
    region_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'regionId'})
    # LocalName is the provider's localized region name.
    local_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'localName'})
    # Endpoint is the public ECS API host for the region.
    endpoint: str = ''
    # Status is available or soldOut.
    status: str = ''


setattr(ECSRegion, '__module__', 'dex_connectors.alibaba.models')


# DescribeRegionsOutput contains the single-page ECS region list.
@dataclass(frozen=True, slots=True)
class DescribeRegionsOutput:
    # Regions contains all returned ECS regions, including sold-out entries.
    regions: tuple[ECSRegion, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'regions', _replace_nil_slices(self.regions, 1))


DescribeRegionsOutput.__annotations__['regions'] = tuple[ECSRegion, ...] | None
DescribeRegionsOutput.__dataclass_fields__['regions'].type = tuple[ECSRegion, ...] | None
setattr(DescribeRegionsOutput, '__module__', 'dex_connectors.alibaba.models')
