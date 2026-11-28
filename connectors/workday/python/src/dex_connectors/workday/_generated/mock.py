# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetWorkerInput, ListWorkersInput, WorkerPage, WorkerSummary
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_WORKER_DEFINITION, INVALID_RESPONSE, LIST_WORKERS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def get_worker_found(value: WorkerSummary, failure: Failure | None = None) -> Case[WorkerSummary]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_worker_found, '__module__', "dex_connectors.workday.workdaymock")

def get_worker_not_found(value: WorkerSummary, failure: Failure | None = None) -> Case[WorkerSummary]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_worker_not_found, '__module__', "dex_connectors.workday.workdaymock")

def get_worker_provider_rejected(value: WorkerSummary, failure: Failure | None = None) -> Case[WorkerSummary]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_worker_provider_rejected, '__module__', "dex_connectors.workday.workdaymock")

def get_worker_invalid_response(value: WorkerSummary, failure: Failure | None = None) -> Case[WorkerSummary]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_worker_invalid_response, '__module__', "dex_connectors.workday.workdaymock")

def get_worker_defect(value: WorkerSummary, failure: Failure | None = None) -> Case[WorkerSummary]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_worker_defect, '__module__', "dex_connectors.workday.workdaymock")

def get_worker_retry(failure: Failure, after_seconds: float = 0) -> Case[WorkerSummary]:
    return Case.retry(failure, after_seconds)

setattr(get_worker_retry, '__module__', "dex_connectors.workday.workdaymock")

def get_worker_found_default() -> Case[WorkerSummary]:
    return Case.branch_result(FOUND, WorkerSummary(id='0123456789abcdef0123456789abcdef', descriptor='Ada Lovelace'))

setattr(get_worker_found_default, '__module__', "dex_connectors.workday.workdaymock")

def list_workers_found(value: WorkerPage, failure: Failure | None = None) -> Case[WorkerPage]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_workers_found, '__module__', "dex_connectors.workday.workdaymock")

def list_workers_provider_rejected(value: WorkerPage, failure: Failure | None = None) -> Case[WorkerPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_workers_provider_rejected, '__module__', "dex_connectors.workday.workdaymock")

def list_workers_invalid_response(value: WorkerPage, failure: Failure | None = None) -> Case[WorkerPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_workers_invalid_response, '__module__', "dex_connectors.workday.workdaymock")

def list_workers_defect(value: WorkerPage, failure: Failure | None = None) -> Case[WorkerPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_workers_defect, '__module__', "dex_connectors.workday.workdaymock")

def list_workers_retry(failure: Failure, after_seconds: float = 0) -> Case[WorkerPage]:
    return Case.retry(failure, after_seconds)

setattr(list_workers_retry, '__module__', "dex_connectors.workday.workdaymock")

def list_workers_found_default() -> Case[WorkerPage]:
    return Case.branch_result(FOUND, WorkerPage(total=1, data=(WorkerSummary(id='0123456789abcdef0123456789abcdef', descriptor='Ada Lovelace'),)))

setattr(list_workers_found_default, '__module__', "dex_connectors.workday.workdaymock")


class Mock:

    def __init__(self, connection_name: str = "workday") -> None:
        self.get_worker_script: Query[GetWorkerInput, WorkerSummary] = Query(GET_WORKER_DEFINITION)
        self.get_worker_script.default(get_worker_found_default())
        self.list_workers_script: Query[ListWorkersInput, WorkerPage] = Query(LIST_WORKERS_DEFINITION)
        self.list_workers_script.default(list_workers_found_default())
        self._connection = new_connection_with_operations(
            get_worker=self.get_worker_script,
            list_workers=self.list_workers_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.workday.workdaymock")

__all__ = [
    "Mock",
    "get_worker_found",
    "get_worker_not_found",
    "get_worker_provider_rejected",
    "get_worker_invalid_response",
    "get_worker_defect",
    "get_worker_retry",
    "get_worker_found_default",
    "list_workers_found",
    "list_workers_provider_rejected",
    "list_workers_invalid_response",
    "list_workers_defect",
    "list_workers_retry",
    "list_workers_found_default",
]
