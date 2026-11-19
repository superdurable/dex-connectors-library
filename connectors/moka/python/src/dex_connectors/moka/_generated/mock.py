# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import JobFunction, ListJobFunctionsInput, ListJobFunctionsOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_JOB_FUNCTIONS_DEFINITION, PROVIDER_REJECTED

def list_job_functions_found(value: ListJobFunctionsOutput, failure: Failure | None = None) -> Case[ListJobFunctionsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_job_functions_found, '__module__', "dex_connectors.moka.mokamock")

def list_job_functions_provider_rejected(value: ListJobFunctionsOutput, failure: Failure | None = None) -> Case[ListJobFunctionsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_job_functions_provider_rejected, '__module__', "dex_connectors.moka.mokamock")

def list_job_functions_invalid_response(value: ListJobFunctionsOutput, failure: Failure | None = None) -> Case[ListJobFunctionsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_job_functions_invalid_response, '__module__', "dex_connectors.moka.mokamock")

def list_job_functions_defect(value: ListJobFunctionsOutput, failure: Failure | None = None) -> Case[ListJobFunctionsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_job_functions_defect, '__module__', "dex_connectors.moka.mokamock")

def list_job_functions_retry(failure: Failure, after_seconds: float = 0) -> Case[ListJobFunctionsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_job_functions_retry, '__module__', "dex_connectors.moka.mokamock")

def list_job_functions_two_functions() -> Case[ListJobFunctionsOutput]:
    return Case.branch_result(FOUND, ListJobFunctionsOutput(functions=(JobFunction(id=2873, label='Technology', parent_id=0, job_count=64), JobFunction(id=2874, label='Product', parent_id=0, job_count=12))))

setattr(list_job_functions_two_functions, '__module__', "dex_connectors.moka.mokamock")


class Mock:

    def __init__(self, connection_name: str = "moka") -> None:
        self.list_job_functions_script: Query[ListJobFunctionsInput, ListJobFunctionsOutput] = Query(LIST_JOB_FUNCTIONS_DEFINITION)
        self.list_job_functions_script.default(list_job_functions_two_functions())
        self._connection = new_connection_with_operations(
            list_job_functions=self.list_job_functions_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.moka.mokamock")

__all__ = [
    "Mock",
    "list_job_functions_found",
    "list_job_functions_provider_rejected",
    "list_job_functions_invalid_response",
    "list_job_functions_defect",
    "list_job_functions_retry",
    "list_job_functions_two_functions",
]
