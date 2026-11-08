# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetTaskInput selects a task by its non-secret Teamwork ID.
@dataclass(frozen=True, slots=True)
class GetTaskInput:
    # TaskID is the ID shown in Teamwork Tasks or returned by the API.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})


setattr(GetTaskInput, '__module__', 'dex_connectors.teamwork.models')


# TaskSummary excludes descriptions and assignee details.
@dataclass(frozen=True, slots=True)
class TaskSummary:
    # ID is the Teamwork task ID.
    id: str = ''
    # Name is the task title.
    name: str = ''
    # Status is the task workflow state such as pending.
    status: str = ''


setattr(TaskSummary, '__module__', 'dex_connectors.teamwork.models')
