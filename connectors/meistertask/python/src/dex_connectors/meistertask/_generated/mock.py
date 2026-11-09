# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetTaskInput, TaskSummary
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_TASK_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_task_found(value: TaskSummary, failure: Failure | None = None) -> Case[TaskSummary]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_task_found, '__module__', "dex_connectors.meistertask.meistertaskmock")

def get_task_not_found(value: TaskSummary, failure: Failure | None = None) -> Case[TaskSummary]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_task_not_found, '__module__', "dex_connectors.meistertask.meistertaskmock")

def get_task_provider_rejected(value: TaskSummary, failure: Failure | None = None) -> Case[TaskSummary]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_task_provider_rejected, '__module__', "dex_connectors.meistertask.meistertaskmock")

def get_task_invalid_response(value: TaskSummary, failure: Failure | None = None) -> Case[TaskSummary]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_task_invalid_response, '__module__', "dex_connectors.meistertask.meistertaskmock")

def get_task_defect(value: TaskSummary, failure: Failure | None = None) -> Case[TaskSummary]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_task_defect, '__module__', "dex_connectors.meistertask.meistertaskmock")

def get_task_retry(failure: Failure, after_seconds: float = 0) -> Case[TaskSummary]:
    return Case.retry(failure, after_seconds)

setattr(get_task_retry, '__module__', "dex_connectors.meistertask.meistertaskmock")

def get_task_found_default() -> Case[TaskSummary]:
    return Case.branch_result(FOUND, TaskSummary(id='15', name='Task Name', status='1'))

setattr(get_task_found_default, '__module__', "dex_connectors.meistertask.meistertaskmock")


class Mock:

    def __init__(self, connection_name: str = "meistertask") -> None:
        self.get_task_script: Query[GetTaskInput, TaskSummary] = Query(GET_TASK_DEFINITION)
        self.get_task_script.default(get_task_found_default())
        self._connection = new_connection_with_operations(
            get_task=self.get_task_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.meistertask.meistertaskmock")

__all__ = [
    "Mock",
    "get_task_found",
    "get_task_not_found",
    "get_task_provider_rejected",
    "get_task_invalid_response",
    "get_task_defect",
    "get_task_retry",
    "get_task_found_default",
]
