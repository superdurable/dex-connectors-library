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


# ListProjectsInput selects a bounded page of accessible projects.
@dataclass(frozen=True, slots=True)
class ListProjectsInput:
    # Top is the maximum number of projects in this page, from 1 to 100.
    top: int = 0
    # Skip is the nonnegative number of projects omitted before this page; zero starts at the first project.
    skip: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListProjectsInput, '__module__', 'dex_connectors.microsoft_azure_devops.models')


# ProjectMetadata contains stable project identity and state.
@dataclass(frozen=True, slots=True)
class ProjectMetadata:
    # ID is the Azure DevOps project UUID.
    id: str = ''
    # Name is the project name shown in Azure DevOps.
    name: str = ''
    # State is the provider's current project state.
    state: str = ''


setattr(ProjectMetadata, '__module__', 'dex_connectors.microsoft_azure_devops.models')


# Projects contains accessible projects without links or descriptions.
@dataclass(frozen=True, slots=True)
class Projects:
    # Projects contains IDs, names, and states.
    projects: tuple[ProjectMetadata, ...] = ()
    # Top is the requested maximum page size.
    top: int = 0
    # Skip is the requested project offset.
    skip: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'projects', _replace_nil_slices(self.projects, 1))


Projects.__annotations__['projects'] = tuple[ProjectMetadata, ...] | None
Projects.__dataclass_fields__['projects'].type = tuple[ProjectMetadata, ...] | None
setattr(Projects, '__module__', 'dex_connectors.microsoft_azure_devops.models')


# GetWorkItemInput selects one work item by numeric ID.
@dataclass(frozen=True, slots=True)
class GetWorkItemInput:
    # WorkItemID is the decimal identifier displayed in Azure DevOps.
    work_item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workItemId'})


setattr(GetWorkItemInput, '__module__', 'dex_connectors.microsoft_azure_devops.models')


# WorkItemMetadata contains a work item's basic identity.
@dataclass(frozen=True, slots=True)
class WorkItemMetadata:
    # ID is the stable work item identifier.
    id: int = 0
    # Title is the work item's title.
    title: str = ''
    # State is the current workflow state.
    state: str = ''
    # WorkItemType is the Azure DevOps work item type.
    work_item_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'workItemType'})


setattr(WorkItemMetadata, '__module__', 'dex_connectors.microsoft_azure_devops.models')
