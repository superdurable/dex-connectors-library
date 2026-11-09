# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetTaskInput selects a task by its non-secret Wrike ID.
@dataclass(frozen=True, slots=True)
class GetTaskInput:
    # TaskID is the ID shown in Wrike Tasks or returned by the API.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})


setattr(GetTaskInput, '__module__', 'dex_connectors.wrike.models')


# TaskStatus excludes descriptions, custom fields, and assignees.
@dataclass(frozen=True, slots=True)
class TaskStatus:
    # ID is the Wrike task ID.
    id: str = ''
    # Title is the task's display title.
    title: str = ''
    # Status is the task state such as Active.
    status: str = ''


setattr(TaskStatus, '__module__', 'dex_connectors.wrike.models')
