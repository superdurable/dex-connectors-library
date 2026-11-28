# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Employee, EmployeesPage, GetEmployeeInput, ListEmployeesInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_EMPLOYEE_DEFINITION, INVALID_RESPONSE, LISTED, LIST_EMPLOYEES_DEFINITION, MISSING, PROVIDER_REJECTED

def get_employee_found(value: Employee, failure: Failure | None = None) -> Case[Employee]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_employee_found, '__module__', "dex_connectors.gusto.gustomock")

def get_employee_missing(value: Employee, failure: Failure | None = None) -> Case[Employee]:
    return Case.branch_result(MISSING, value, failure)

setattr(get_employee_missing, '__module__', "dex_connectors.gusto.gustomock")

def get_employee_provider_rejected(value: Employee, failure: Failure | None = None) -> Case[Employee]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_employee_provider_rejected, '__module__', "dex_connectors.gusto.gustomock")

def get_employee_invalid_response(value: Employee, failure: Failure | None = None) -> Case[Employee]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_employee_invalid_response, '__module__', "dex_connectors.gusto.gustomock")

def get_employee_defect(value: Employee, failure: Failure | None = None) -> Case[Employee]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_employee_defect, '__module__', "dex_connectors.gusto.gustomock")

def get_employee_retry(failure: Failure, after_seconds: float = 0) -> Case[Employee]:
    return Case.retry(failure, after_seconds)

setattr(get_employee_retry, '__module__', "dex_connectors.gusto.gustomock")

def get_employee_found_default() -> Case[Employee]:
    return Case.branch_result(FOUND, Employee(employee_id='90a68db5-cb3a-4f64-bb60-c0a934ee7600', first_name='Ada', last_name='Lovelace', email='ada@example.com', company_id='7d61f73e-a18c-46a5-88e2-2bfa894657d7'))

setattr(get_employee_found_default, '__module__', "dex_connectors.gusto.gustomock")

def list_employees_listed(value: EmployeesPage, failure: Failure | None = None) -> Case[EmployeesPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_employees_listed, '__module__', "dex_connectors.gusto.gustomock")

def list_employees_missing(value: EmployeesPage, failure: Failure | None = None) -> Case[EmployeesPage]:
    return Case.branch_result(MISSING, value, failure)

setattr(list_employees_missing, '__module__', "dex_connectors.gusto.gustomock")

def list_employees_provider_rejected(value: EmployeesPage, failure: Failure | None = None) -> Case[EmployeesPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_employees_provider_rejected, '__module__', "dex_connectors.gusto.gustomock")

def list_employees_invalid_response(value: EmployeesPage, failure: Failure | None = None) -> Case[EmployeesPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_employees_invalid_response, '__module__', "dex_connectors.gusto.gustomock")

def list_employees_defect(value: EmployeesPage, failure: Failure | None = None) -> Case[EmployeesPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_employees_defect, '__module__', "dex_connectors.gusto.gustomock")

def list_employees_retry(failure: Failure, after_seconds: float = 0) -> Case[EmployeesPage]:
    return Case.retry(failure, after_seconds)

setattr(list_employees_retry, '__module__', "dex_connectors.gusto.gustomock")

def list_employees_listed_default() -> Case[EmployeesPage]:
    return Case.branch_result(LISTED, EmployeesPage(employees=(Employee(employee_id='90a68db5-cb3a-4f64-bb60-c0a934ee7600', first_name='Ada', last_name='Lovelace', email='ada@example.com', company_id='7d61f73e-a18c-46a5-88e2-2bfa894657d7'),)))

setattr(list_employees_listed_default, '__module__', "dex_connectors.gusto.gustomock")


class Mock:

    def __init__(self, connection_name: str = "gusto") -> None:
        self.get_employee_script: Query[GetEmployeeInput, Employee] = Query(GET_EMPLOYEE_DEFINITION)
        self.get_employee_script.default(get_employee_found_default())
        self.list_employees_script: Query[ListEmployeesInput, EmployeesPage] = Query(LIST_EMPLOYEES_DEFINITION)
        self.list_employees_script.default(list_employees_listed_default())
        self._connection = new_connection_with_operations(
            get_employee=self.get_employee_script,
            list_employees=self.list_employees_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.gusto.gustomock")

__all__ = [
    "Mock",
    "get_employee_found",
    "get_employee_missing",
    "get_employee_provider_rejected",
    "get_employee_invalid_response",
    "get_employee_defect",
    "get_employee_retry",
    "get_employee_found_default",
    "list_employees_listed",
    "list_employees_missing",
    "list_employees_provider_rejected",
    "list_employees_invalid_response",
    "list_employees_defect",
    "list_employees_retry",
    "list_employees_listed_default",
]
