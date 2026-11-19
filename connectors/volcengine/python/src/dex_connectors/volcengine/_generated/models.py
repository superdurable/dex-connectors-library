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


# DescribeRegionsInput selects one ECS region page. MaxResults zero uses the provider default of 10.
@dataclass(frozen=True, slots=True)
class DescribeRegionsInput:
    # MaxResults is the requested page size from 1 to 20; zero uses 10.
    max_results: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxResults', 'dex_json_omit_default': True})
    # NextToken is the opaque pagination token from a previous response; empty starts at the first page.
    next_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextToken', 'dex_json_omit_default': True})


setattr(DescribeRegionsInput, '__module__', 'dex_connectors.volcengine.models')


# DescribeRegionsOutput contains one page of region IDs and an optional continuation token.
@dataclass(frozen=True, slots=True)
class DescribeRegionsOutput:
    # RegionIDs are ECS region IDs without account resource details.
    region_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'regionIds'})
    # NextToken is passed as the next call's NextToken; empty means the last page.
    next_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'region_ids', _replace_nil_slices(self.region_ids, 1))


DescribeRegionsOutput.__annotations__['region_ids'] = tuple[str, ...] | None
DescribeRegionsOutput.__dataclass_fields__['region_ids'].type = tuple[str, ...] | None
setattr(DescribeRegionsOutput, '__module__', 'dex_connectors.volcengine.models')
