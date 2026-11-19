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


# ListDashboardsPageInput selects one provider page; zeros use page 1 and size 10.
@dataclass(frozen=True, slots=True)
class ListDashboardsPageInput:
    # PageIndex is the one-based page number; zero means the first page.
    page_index: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageIndex'})
    # PageSize is the number of dashboards per page, from 1 to 100; zero means 10.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListDashboardsPageInput, '__module__', 'dex_connectors.guance.models')


# DashboardMetadata identifies a dashboard without user details or chart content.
@dataclass(frozen=True, slots=True)
class DashboardMetadata:
    # UUID is the stable dashboard identifier.
    uuid: str = ''
    # Name is the dashboard display name.
    name: str = ''


setattr(DashboardMetadata, '__module__', 'dex_connectors.guance.models')


# ListDashboardsPageOutput contains one page of safe dashboard metadata.
@dataclass(frozen=True, slots=True)
class ListDashboardsPageOutput:
    # TotalCount is the total number of matching dashboards.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})
    # PageIndex is the one-based page number returned by Guance.
    page_index: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageIndex'})
    # PageSize is the page size returned by Guance.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})
    # Dashboards are the identifiers and names in this page.
    dashboards: tuple[DashboardMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'dashboards', _replace_nil_slices(self.dashboards, 1))


ListDashboardsPageOutput.__annotations__['dashboards'] = tuple[DashboardMetadata, ...] | None
ListDashboardsPageOutput.__dataclass_fields__['dashboards'].type = tuple[DashboardMetadata, ...] | None
setattr(ListDashboardsPageOutput, '__module__', 'dex_connectors.guance.models')
