# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetTaskInput selects a task by its non-secret MeisterTask ID.
@dataclass(frozen=True, slots=True)
class GetTaskInput:
    # TaskID is the ID shown in MeisterTask Tasks or returned by the API.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})


setattr(GetTaskInput, '__module__', 'dex_connectors.meistertask.models')


# TaskSummary contains the task's ID, name, and numeric status code.
@dataclass(frozen=True, slots=True)
class TaskSummary:
    # ID is the MeisterTask task ID.
    id: str = ''
    # Name is the task name.
    name: str = ''
    # Status is MeisterTask's numeric status code represented as decimal text.
    status: str = ''


setattr(TaskSummary, '__module__', 'dex_connectors.meistertask.models')
