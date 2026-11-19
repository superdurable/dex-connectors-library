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


# GetTaskStatusInput identifies an existing Vidu generation task.
@dataclass(frozen=True, slots=True)
class GetTaskStatusInput:
    # TaskID is the ID returned as task_id by a task-creation API.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})


setattr(GetTaskStatusInput, '__module__', 'dex_connectors.shengshu.models')


# GetTaskStatusOutput omits provider payload and signed media URLs.
@dataclass(frozen=True, slots=True)
class GetTaskStatusOutput:
    # TaskID is the Vidu task identifier.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # State is created, queueing, processing, success, or failed.
    state: str = ''
    # ErrorCode is Vidu's task failure code, if any.
    error_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'errorCode'})
    # Credits is the number of credits consumed by the task.
    credits: int = 0
    # CreationIDs identifies produced media without exposing signed URLs.
    creation_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'creationIds'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'creation_ids', _replace_nil_slices(self.creation_ids, 1))


GetTaskStatusOutput.__annotations__['creation_ids'] = tuple[str, ...] | None
GetTaskStatusOutput.__dataclass_fields__['creation_ids'].type = tuple[str, ...] | None
setattr(GetTaskStatusOutput, '__module__', 'dex_connectors.shengshu.models')
