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


# GetWorkerInput selects a worker by its non-secret Rippling ID.
@dataclass(frozen=True, slots=True)
class GetWorkerInput:
    # WorkerID is the ID shown in Rippling Workers or returned by the API.
    worker_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workerId'})


setattr(GetWorkerInput, '__module__', 'dex_connectors.rippling.models')


# WorkerSummary excludes personal and compensation details.
@dataclass(frozen=True, slots=True)
class WorkerSummary:
    # ID is the Rippling worker ID.
    id: str = ''
    # Title is the visible worker job title, or empty when unavailable.
    title: str = ''
    # Status is the visible employment state, or empty when unavailable.
    status: str = ''


setattr(WorkerSummary, '__module__', 'dex_connectors.rippling.models')


# ListWorkersInput selects one page of visible worker summaries.
@dataclass(frozen=True, slots=True)
class ListWorkersInput:
    # Limit is the number of workers to return, from 1 to 100. Zero uses Rippling's default.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Cursor is the opaque pagination cursor returned by an earlier page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListWorkersInput, '__module__', 'dex_connectors.rippling.models')


# WorkerPage contains visible worker summaries and the next opaque cursor.
@dataclass(frozen=True, slots=True)
class WorkerPage:
    # Workers contains only IDs, statuses, and titles visible to the token owner.
    workers: tuple[WorkerSummary, ...] = ()
    # NextCursor is empty when Rippling reports no further page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'workers', _replace_nil_slices(self.workers, 1))


WorkerPage.__annotations__['workers'] = tuple[WorkerSummary, ...] | None
WorkerPage.__dataclass_fields__['workers'].type = tuple[WorkerSummary, ...] | None
setattr(WorkerPage, '__module__', 'dex_connectors.rippling.models')
