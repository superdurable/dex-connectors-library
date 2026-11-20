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


# ListInstancesInput selects a one-based page and number of instances per page.
@dataclass(frozen=True, slots=True)
class ListInstancesInput:
    # PageNumber defaults to 1 when omitted; valid values are positive.
    page_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNumber', 'dex_json_omit_default': True})
    # PageSize defaults to 10 when omitted; valid values are 1 through 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})


setattr(ListInstancesInput, '__module__', 'dex_connectors.oceanbase.models')


# InstanceInfo contains selected non-secret cluster fields.
@dataclass(frozen=True, slots=True)
class InstanceInfo:
    # ID is the provider's cluster ID.
    id: str = ''
    # Name is the cluster display name.
    name: str = ''
    # State is the provider's cluster state.
    state: str = ''
    # Region is the cluster deployment region.
    region: str = ''


setattr(InstanceInfo, '__module__', 'dex_connectors.oceanbase.models')


# ListInstancesOutput contains one page and the provider's total count.
@dataclass(frozen=True, slots=True)
class ListInstancesOutput:
    # Items contains this page's instances.
    items: tuple[InstanceInfo, ...] = ()
    # Total is the total number of accessible instances in the project.
    total: int = 0
    # PageNumber is the requested one-based page.
    page_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNumber'})
    # PageSize is the requested maximum number of instances.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'items', _replace_nil_slices(self.items, 1))


ListInstancesOutput.__annotations__['items'] = tuple[InstanceInfo, ...] | None
ListInstancesOutput.__dataclass_fields__['items'].type = tuple[InstanceInfo, ...] | None
setattr(ListInstancesOutput, '__module__', 'dex_connectors.oceanbase.models')
