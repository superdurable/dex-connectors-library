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


# ListProjectsInput selects a one-based page and number of projects per page.
@dataclass(frozen=True, slots=True)
class ListProjectsInput:
    # Page defaults to 1 when omitted; valid values are positive.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PageSize defaults to 10 when omitted; valid values are 1 through 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})


setattr(ListProjectsInput, '__module__', 'dex_connectors.pingcap.models')


# ProjectInfo is one TiDB Cloud project accessible to the API key.
@dataclass(frozen=True, slots=True)
class ProjectInfo:
    # ID is the stable project identifier.
    id: str = ''
    # OrgID is the owning organization identifier.
    org_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'orgId'})
    # Name is the project display name.
    name: str = ''
    # ClusterCount is the number of clusters in the project.
    cluster_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'clusterCount'})
    # UserCount is the number of project users.
    user_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'userCount'})
    # CreateTimestamp is the provider timestamp string.
    create_timestamp: str = _dataclass_field(default='', metadata={'dex_json_name': 'createTimestamp'})
    # Type is the provider project type when supplied.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ProjectInfo, '__module__', 'dex_connectors.pingcap.models')


# ListProjectsOutput contains one page and the provider's total count.
@dataclass(frozen=True, slots=True)
class ListProjectsOutput:
    # Items contains this page's projects.
    items: tuple[ProjectInfo, ...] = ()
    # Total is the total number of accessible projects.
    total: int = 0
    # Page is the requested one-based page.
    page: int = 0
    # PageSize is the requested maximum number of projects.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'items', _replace_nil_slices(self.items, 1))


ListProjectsOutput.__annotations__['items'] = tuple[ProjectInfo, ...] | None
ListProjectsOutput.__dataclass_fields__['items'].type = tuple[ProjectInfo, ...] | None
setattr(ListProjectsOutput, '__module__', 'dex_connectors.pingcap.models')
