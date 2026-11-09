# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

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
