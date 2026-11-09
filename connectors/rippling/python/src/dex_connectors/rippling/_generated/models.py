# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

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
