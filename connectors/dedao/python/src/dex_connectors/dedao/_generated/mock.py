# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetNoteTaskStatusInput, GetNoteTaskStatusOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_NOTE_TASK_STATUS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_note_task_status_found(value: GetNoteTaskStatusOutput, failure: Failure | None = None) -> Case[GetNoteTaskStatusOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_note_task_status_found, '__module__', "dex_connectors.dedao.dedaomock")

def get_note_task_status_provider_rejected(value: GetNoteTaskStatusOutput, failure: Failure | None = None) -> Case[GetNoteTaskStatusOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_note_task_status_provider_rejected, '__module__', "dex_connectors.dedao.dedaomock")

def get_note_task_status_invalid_response(value: GetNoteTaskStatusOutput, failure: Failure | None = None) -> Case[GetNoteTaskStatusOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_note_task_status_invalid_response, '__module__', "dex_connectors.dedao.dedaomock")

def get_note_task_status_defect(value: GetNoteTaskStatusOutput, failure: Failure | None = None) -> Case[GetNoteTaskStatusOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_note_task_status_defect, '__module__', "dex_connectors.dedao.dedaomock")

def get_note_task_status_retry(failure: Failure, after_seconds: float = 0) -> Case[GetNoteTaskStatusOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_note_task_status_retry, '__module__', "dex_connectors.dedao.dedaomock")

def get_note_task_status_completed_task() -> Case[GetNoteTaskStatusOutput]:
    return Case.branch_result(FOUND, GetNoteTaskStatusOutput(task_id='69c3995e99f5a67e', status='success', note_id='1907975006118548200'))

setattr(get_note_task_status_completed_task, '__module__', "dex_connectors.dedao.dedaomock")

def get_note_task_status_pending_task() -> Case[GetNoteTaskStatusOutput]:
    return Case.branch_result(FOUND, GetNoteTaskStatusOutput(task_id='69c3995e99f5a67e', status='pending', note_id=''))

setattr(get_note_task_status_pending_task, '__module__', "dex_connectors.dedao.dedaomock")


class Mock:

    def __init__(self, connection_name: str = "dedao") -> None:
        self.get_note_task_status_script: Query[GetNoteTaskStatusInput, GetNoteTaskStatusOutput] = Query(GET_NOTE_TASK_STATUS_DEFINITION)
        self.get_note_task_status_script.default(get_note_task_status_completed_task())
        self._connection = new_connection_with_operations(
            get_note_task_status=self.get_note_task_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.dedao.dedaomock")

__all__ = [
    "Mock",
    "get_note_task_status_found",
    "get_note_task_status_provider_rejected",
    "get_note_task_status_invalid_response",
    "get_note_task_status_defect",
    "get_note_task_status_retry",
    "get_note_task_status_completed_task",
    "get_note_task_status_pending_task",
]
