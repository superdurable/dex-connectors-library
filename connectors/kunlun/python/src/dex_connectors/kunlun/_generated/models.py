# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetTaskStatusInput identifies an existing Mureka song-generation task.
@dataclass(frozen=True, slots=True)
class GetTaskStatusInput:
    # TaskID is the id returned by a song-generation request.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})


setattr(GetTaskStatusInput, '__module__', 'dex_connectors.kunlun.models')


# GetTaskStatusOutput contains task metadata without generated lyrics or media URLs.
@dataclass(frozen=True, slots=True)
class GetTaskStatusOutput:
    # TaskID is Mureka's task identifier.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # Status is the observed song-generation state.
    status: str = ''
    # Model identifies the model used for generation.
    model: str = ''


setattr(GetTaskStatusOutput, '__module__', 'dex_connectors.kunlun.models')
