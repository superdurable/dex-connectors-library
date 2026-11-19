# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DepartmentSummary, ListDepartmentsInput, ListDepartmentsOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_DEPARTMENTS_DEFINITION, PROVIDER_REJECTED

def list_departments_listed(value: ListDepartmentsOutput, failure: Failure | None = None) -> Case[ListDepartmentsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_departments_listed, '__module__', "dex_connectors.hesi.hesimock")

def list_departments_provider_rejected(value: ListDepartmentsOutput, failure: Failure | None = None) -> Case[ListDepartmentsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_departments_provider_rejected, '__module__', "dex_connectors.hesi.hesimock")

def list_departments_invalid_response(value: ListDepartmentsOutput, failure: Failure | None = None) -> Case[ListDepartmentsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_departments_invalid_response, '__module__', "dex_connectors.hesi.hesimock")

def list_departments_defect(value: ListDepartmentsOutput, failure: Failure | None = None) -> Case[ListDepartmentsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_departments_defect, '__module__', "dex_connectors.hesi.hesimock")

def list_departments_retry(failure: Failure, after_seconds: float = 0) -> Case[ListDepartmentsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_departments_retry, '__module__', "dex_connectors.hesi.hesimock")

def list_departments_sample_departments() -> Case[ListDepartmentsOutput]:
    return Case.branch_result(LISTED, ListDepartmentsOutput(departments=(DepartmentSummary(id='corp:dept', name='Example Department', code='001', active=True),), total=1, next_start=0))

setattr(list_departments_sample_departments, '__module__', "dex_connectors.hesi.hesimock")


class Mock:

    def __init__(self, connection_name: str = "hesi") -> None:
        self.list_departments_script: Query[ListDepartmentsInput, ListDepartmentsOutput] = Query(LIST_DEPARTMENTS_DEFINITION)
        self.list_departments_script.default(list_departments_sample_departments())
        self._connection = new_connection_with_operations(
            list_departments=self.list_departments_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.hesi.hesimock")

__all__ = [
    "Mock",
    "list_departments_listed",
    "list_departments_provider_rejected",
    "list_departments_invalid_response",
    "list_departments_defect",
    "list_departments_retry",
    "list_departments_sample_departments",
]
