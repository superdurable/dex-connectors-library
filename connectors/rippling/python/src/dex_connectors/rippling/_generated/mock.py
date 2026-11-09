# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetWorkerInput, WorkerSummary
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_WORKER_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_worker_found(value: WorkerSummary, failure: Failure | None = None) -> Case[WorkerSummary]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_worker_found, '__module__', "dex_connectors.rippling.ripplingmock")

def get_worker_not_found(value: WorkerSummary, failure: Failure | None = None) -> Case[WorkerSummary]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_worker_not_found, '__module__', "dex_connectors.rippling.ripplingmock")

def get_worker_provider_rejected(value: WorkerSummary, failure: Failure | None = None) -> Case[WorkerSummary]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_worker_provider_rejected, '__module__', "dex_connectors.rippling.ripplingmock")

def get_worker_invalid_response(value: WorkerSummary, failure: Failure | None = None) -> Case[WorkerSummary]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_worker_invalid_response, '__module__', "dex_connectors.rippling.ripplingmock")

def get_worker_defect(value: WorkerSummary, failure: Failure | None = None) -> Case[WorkerSummary]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_worker_defect, '__module__', "dex_connectors.rippling.ripplingmock")

def get_worker_retry(failure: Failure, after_seconds: float = 0) -> Case[WorkerSummary]:
    return Case.retry(failure, after_seconds)

setattr(get_worker_retry, '__module__', "dex_connectors.rippling.ripplingmock")

def get_worker_found_default() -> Case[WorkerSummary]:
    return Case.branch_result(FOUND, WorkerSummary(id='65699281fb7bc7c3510f44a6', title='Engineer', status='ACTIVE'))

setattr(get_worker_found_default, '__module__', "dex_connectors.rippling.ripplingmock")


class Mock:

    def __init__(self, connection_name: str = "rippling") -> None:
        self.get_worker_script: Query[GetWorkerInput, WorkerSummary] = Query(GET_WORKER_DEFINITION)
        self.get_worker_script.default(get_worker_found_default())
        self._connection = new_connection_with_operations(
            get_worker=self.get_worker_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.rippling.ripplingmock")

__all__ = [
    "Mock",
    "get_worker_found",
    "get_worker_not_found",
    "get_worker_provider_rejected",
    "get_worker_invalid_response",
    "get_worker_defect",
    "get_worker_retry",
    "get_worker_found_default",
]
