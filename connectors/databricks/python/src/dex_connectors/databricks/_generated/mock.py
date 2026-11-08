# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetJobRunInput, JobRunStatus
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_JOB_RUN_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_job_run_found(value: JobRunStatus, failure: Failure | None = None) -> Case[JobRunStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_job_run_found, '__module__', "dex_connectors.databricks.databricksmock")

def get_job_run_not_found(value: JobRunStatus, failure: Failure | None = None) -> Case[JobRunStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_job_run_not_found, '__module__', "dex_connectors.databricks.databricksmock")

def get_job_run_provider_rejected(value: JobRunStatus, failure: Failure | None = None) -> Case[JobRunStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_job_run_provider_rejected, '__module__', "dex_connectors.databricks.databricksmock")

def get_job_run_invalid_response(value: JobRunStatus, failure: Failure | None = None) -> Case[JobRunStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_job_run_invalid_response, '__module__', "dex_connectors.databricks.databricksmock")

def get_job_run_defect(value: JobRunStatus, failure: Failure | None = None) -> Case[JobRunStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_job_run_defect, '__module__', "dex_connectors.databricks.databricksmock")

def get_job_run_retry(failure: Failure, after_seconds: float = 0) -> Case[JobRunStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_job_run_retry, '__module__', "dex_connectors.databricks.databricksmock")

def get_job_run_found_default() -> Case[JobRunStatus]:
    return Case.branch_result(FOUND, JobRunStatus(id='455644833', result_state='', status='RUNNING'))

setattr(get_job_run_found_default, '__module__', "dex_connectors.databricks.databricksmock")


class Mock:

    def __init__(self, connection_name: str = "databricks") -> None:
        self.get_job_run_script: Query[GetJobRunInput, JobRunStatus] = Query(GET_JOB_RUN_DEFINITION)
        self.get_job_run_script.default(get_job_run_found_default())
        self._connection = new_connection_with_operations(
            get_job_run=self.get_job_run_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.databricks.databricksmock")

__all__ = [
    "Mock",
    "get_job_run_found",
    "get_job_run_not_found",
    "get_job_run_provider_rejected",
    "get_job_run_invalid_response",
    "get_job_run_defect",
    "get_job_run_retry",
    "get_job_run_found_default",
]
