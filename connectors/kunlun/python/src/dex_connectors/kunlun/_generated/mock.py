# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetTaskStatusInput, GetTaskStatusOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_TASK_STATUS_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_task_status_found(value: GetTaskStatusOutput, failure: Failure | None = None) -> Case[GetTaskStatusOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_task_status_found, '__module__', "dex_connectors.kunlun.kunlunmock")

def get_task_status_not_found(value: GetTaskStatusOutput, failure: Failure | None = None) -> Case[GetTaskStatusOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_task_status_not_found, '__module__', "dex_connectors.kunlun.kunlunmock")

def get_task_status_provider_rejected(value: GetTaskStatusOutput, failure: Failure | None = None) -> Case[GetTaskStatusOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_task_status_provider_rejected, '__module__', "dex_connectors.kunlun.kunlunmock")

def get_task_status_invalid_response(value: GetTaskStatusOutput, failure: Failure | None = None) -> Case[GetTaskStatusOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_task_status_invalid_response, '__module__', "dex_connectors.kunlun.kunlunmock")

def get_task_status_defect(value: GetTaskStatusOutput, failure: Failure | None = None) -> Case[GetTaskStatusOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_task_status_defect, '__module__', "dex_connectors.kunlun.kunlunmock")

def get_task_status_retry(failure: Failure, after_seconds: float = 0) -> Case[GetTaskStatusOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_task_status_retry, '__module__', "dex_connectors.kunlun.kunlunmock")

def get_task_status_completed_task() -> Case[GetTaskStatusOutput]:
    return Case.branch_result(FOUND, GetTaskStatusOutput(task_id='435134', status='succeeded', model='mureka-6'))

setattr(get_task_status_completed_task, '__module__', "dex_connectors.kunlun.kunlunmock")


class Mock:

    def __init__(self, connection_name: str = "kunlun") -> None:
        self.get_task_status_script: Query[GetTaskStatusInput, GetTaskStatusOutput] = Query(GET_TASK_STATUS_DEFINITION)
        self.get_task_status_script.default(get_task_status_completed_task())
        self._connection = new_connection_with_operations(
            get_task_status=self.get_task_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.kunlun.kunlunmock")

__all__ = [
    "Mock",
    "get_task_status_found",
    "get_task_status_not_found",
    "get_task_status_provider_rejected",
    "get_task_status_invalid_response",
    "get_task_status_defect",
    "get_task_status_retry",
    "get_task_status_completed_task",
]
