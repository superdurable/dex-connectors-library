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


# GetTaskInput identifies an active, non-completed task.
@dataclass(frozen=True, slots=True)
class GetTaskInput:
    # TaskID is Todoist's opaque string task ID, not a task URL.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})


setattr(GetTaskInput, '__module__', 'dex_connectors.doist_todoist.models')


# Task is a bounded projection of Todoist's API v1 task response. Provider text is untrusted.
@dataclass(frozen=True, slots=True)
class Task:
    # ID is the opaque task ID.
    id: str = ''
    # ProjectID identifies the task's project.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})
    # Content is the task title.
    content: str = ''
    # Description is the task description, possibly empty.
    description: str = ''
    # IsCompleted reports Todoist's checked field.
    is_completed: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCompleted'})


setattr(Task, '__module__', 'dex_connectors.doist_todoist.models')


# UpdateTaskInput patches specified fields; nil preserves a field. At least one field must be specified.
@dataclass(frozen=True, slots=True)
class UpdateTaskInput:
    # TaskID identifies the existing task.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # Content is an optional non-blank replacement title. Nil leaves the title unchanged.
    content: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Description replaces the description when non-nil. An empty string clears it.
    description: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(UpdateTaskInput, '__module__', 'dex_connectors.doist_todoist.models')


# ListTasksInput selects one page; retain identical filters when following NextCursor.
@dataclass(frozen=True, slots=True)
class ListTasksInput:
    # ProjectID limits results to one project. Empty lists active tasks across projects.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId', 'dex_json_omit_default': True})
    # Cursor is the previous page's opaque NextCursor. Empty starts the first page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Limit is the page size, 1 through 200. Zero uses Todoist's default of 50.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListTasksInput, '__module__', 'dex_connectors.doist_todoist.models')


# TaskPage contains active tasks and a continuation cursor; it is not a consistent snapshot.
@dataclass(frozen=True, slots=True)
class TaskPage:
    # Tasks contains this page, possibly empty.
    tasks: tuple[Task, ...] = ()
    # NextCursor is empty when there is no next page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tasks', _replace_nil_slices(self.tasks, 1))


TaskPage.__annotations__['tasks'] = tuple[Task, ...] | None
TaskPage.__dataclass_fields__['tasks'].type = tuple[Task, ...] | None
setattr(TaskPage, '__module__', 'dex_connectors.doist_todoist.models')


# CreateTaskInput creates a task; omitted fields use Todoist defaults.
@dataclass(frozen=True, slots=True)
class CreateTaskInput:
    # Content is the complete non-blank UTF-8 task title.
    content: str = ''
    # Description is optional UTF-8 description text.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ProjectID chooses a project. Empty creates the task in Inbox.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId', 'dex_json_omit_default': True})


setattr(CreateTaskInput, '__module__', 'dex_connectors.doist_todoist.models')


# DeleteTaskInput identifies the task to delete, including its subtasks.
@dataclass(frozen=True, slots=True)
class DeleteTaskInput:
    # TaskID is the immutable task ID; never a title or search expression.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})


setattr(DeleteTaskInput, '__module__', 'dex_connectors.doist_todoist.models')


# DeleteTaskOutcome identifies the requested task; inspect the branch to distinguish deletion from absence.
@dataclass(frozen=True, slots=True)
class DeleteTaskOutcome:
    # TaskID is the task requested for deletion.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})


setattr(DeleteTaskOutcome, '__module__', 'dex_connectors.doist_todoist.models')
