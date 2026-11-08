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


# ListTaskListsInput requests the signed-in person's task lists.
@dataclass(frozen=True, slots=True)
class ListTaskListsInput:
    pass


setattr(ListTaskListsInput, '__module__', 'dex_connectors.microsoft_todo.models')


# TaskList identifies one To Do list.
@dataclass(frozen=True, slots=True)
class TaskList:
    # ID is the stable Graph task-list ID.
    id: str = ''
    # DisplayName is the user-visible list name.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName'})


setattr(TaskList, '__module__', 'dex_connectors.microsoft_todo.models')


# TaskListsPage contains a complete bounded collection of task lists.
@dataclass(frozen=True, slots=True)
class TaskListsPage:
    # Lists contains every fetched list, up to the documented safety limit.
    lists: tuple[TaskList, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'lists', _replace_nil_slices(self.lists, 1))


TaskListsPage.__annotations__['lists'] = tuple[TaskList, ...] | None
TaskListsPage.__dataclass_fields__['lists'].type = tuple[TaskList, ...] | None
setattr(TaskListsPage, '__module__', 'dex_connectors.microsoft_todo.models')


# ListTasksInput selects a task list returned by ListTaskLists.
@dataclass(frozen=True, slots=True)
class ListTasksInput:
    # ListID is the stable non-secret ID returned by ListTaskLists.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})


setattr(ListTasksInput, '__module__', 'dex_connectors.microsoft_todo.models')


# Task describes one selected To Do task.
@dataclass(frozen=True, slots=True)
class Task:
    # ID is the stable Graph task ID.
    id: str = ''
    # Title is the task title entered in To Do.
    title: str = ''
    # Status is the Graph task status, such as notStarted or completed.
    status: str = ''


setattr(Task, '__module__', 'dex_connectors.microsoft_todo.models')


# TasksPage contains a complete bounded collection of tasks.
@dataclass(frozen=True, slots=True)
class TasksPage:
    # Tasks contains every fetched task, up to the documented safety limit.
    tasks: tuple[Task, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tasks', _replace_nil_slices(self.tasks, 1))


TasksPage.__annotations__['tasks'] = tuple[Task, ...] | None
TasksPage.__dataclass_fields__['tasks'].type = tuple[Task, ...] | None
setattr(TasksPage, '__module__', 'dex_connectors.microsoft_todo.models')
