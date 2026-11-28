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


# GetWorkerInput identifies a worker by its Workday ID.
@dataclass(frozen=True, slots=True)
class GetWorkerInput:
    # WorkerID is the WID returned by the Workday workers REST endpoint.
    worker_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workerId'})


setattr(GetWorkerInput, '__module__', 'dex_connectors.workday.models')


# WorkerSummary contains the stable WID and displayed worker name.
@dataclass(frozen=True, slots=True)
class WorkerSummary:
    # ID is the worker's Workday ID.
    id: str = ''
    # Descriptor is the worker's display name.
    descriptor: str = ''


setattr(WorkerSummary, '__module__', 'dex_connectors.workday.models')


# ListWorkersInput selects a page of workers and an optional name-prefix search.
@dataclass(frozen=True, slots=True)
class ListWorkersInput:
    # Limit is the page size from 1 to 100; zero uses Workday's default of 20.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Offset is the zero-based position of the first worker.
    offset: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Search is at least three consecutive characters from a worker name when set.
    search: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListWorkersInput, '__module__', 'dex_connectors.workday.models')


# WorkerPage contains one page and the total count of accessible workers.
@dataclass(frozen=True, slots=True)
class WorkerPage:
    # Total is the number of accessible workers matching the search.
    total: int = 0
    # Data contains the workers in this page.
    data: tuple[WorkerSummary, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'data', _replace_nil_slices(self.data, 1))


WorkerPage.__annotations__['data'] = tuple[WorkerSummary, ...] | None
WorkerPage.__dataclass_fields__['data'].type = tuple[WorkerSummary, ...] | None
setattr(WorkerPage, '__module__', 'dex_connectors.workday.models')
