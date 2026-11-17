# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetNoteTaskStatusInput identifies an existing asynchronous note-save task.
@dataclass(frozen=True, slots=True)
class GetNoteTaskStatusInput:
    # TaskID is data.tasks[0].task_id returned by a prior note-save call.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskID'})


setattr(GetNoteTaskStatusInput, '__module__', 'dex_connectors.dedao.models')


# GetNoteTaskStatusOutput contains status and the resulting note ID without note content.
@dataclass(frozen=True, slots=True)
class GetNoteTaskStatusOutput:
    # TaskID is the requested asynchronous task identifier.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskID'})
    # Status is pending, processing, success, or failed.
    status: str = ''
    # NoteID is set when the provider returns one for a successful task.
    note_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'noteID'})


setattr(GetNoteTaskStatusOutput, '__module__', 'dex_connectors.dedao.models')
