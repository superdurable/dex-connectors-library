# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gusto employee queries with bounded responses and renewable credentials."""

from __future__ import annotations

import json
import re
from typing import TypeVar

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import Employee, EmployeesPage, GetEmployeeInput, ListEmployeesInput

_EMPLOYEE_UUID = re.compile(r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")


_Output = TypeVar("_Output")


def _branch(
    operation: str, empty: _Output, branch: str, kind: FailureKind, message: str
) -> QueryAttempt[_Output]:
    return QueryAttempt.branch_result(branch, empty, Receipt(), Failure(kind, "gusto", operation, message))


def _read_failure(exchange: Exchange, operation: str, empty: _Output) -> QueryAttempt[_Output] | None:
    def retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[_Output]:
        return QueryAttempt.retry(Failure(kind, "gusto", operation, message), delay)

    if exchange.error == "credential":
        return _branch(
            operation,
            empty,
            "defect",
            FailureKind.AUTHENTICATION,
            "Gusto OAuth credentials are unavailable or invalid",
        )
    if exchange.error in ("request", "refresh"):
        return retry(FailureKind.TRANSPORT, "Gusto request failed")
    if exchange.status == 429:
        return retry(FailureKind.RATE_LIMIT, "Gusto rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return retry(FailureKind.AVAILABILITY, "Gusto is temporarily unavailable", exchange.retry_after)
    if exchange.error == "too_large":
        return _branch(
            operation,
            empty,
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Gusto response exceeded the configured limit",
        )
    if exchange.error:
        return retry(FailureKind.TRANSPORT, "Gusto response was interrupted")
    if exchange.status == 404:
        return QueryAttempt.branch_result("missing", empty, Receipt())
    if exchange.status != 200:
        return _branch(
            operation,
            empty,
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Gusto rejected HTTP {exchange.status}",
        )
    return None


def _read_json(body: bytes) -> object:
    try:
        return json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None


def _decode_employee(record: object) -> Employee | None:
    if not isinstance(record, dict):
        return None
    employee_id = record.get("uuid")
    first_name = record.get("first_name")
    last_name = record.get("last_name")
    email = record.get("email")
    company_id = record.get("company_uuid")
    if (
        not isinstance(employee_id, str)
        or not _EMPLOYEE_UUID.fullmatch(employee_id)
        or not isinstance(first_name, str)
        or not first_name.strip()
        or not isinstance(last_name, str)
        or not last_name.strip()
        or email is not None
        and not isinstance(email, str)
        or not isinstance(company_id, str)
        or not _EMPLOYEE_UUID.fullmatch(company_id)
    ):
        return None
    return Employee(employee_id, first_name, last_name, email or "", company_id)


class GetEmployeeOperation:
    """Read one employee from the authorized Gusto company."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated employee Query contract."""
        return definitions.GET_EMPLOYEE_DEFINITION

    async def invoke(self, call: Call, input: GetEmployeeInput) -> QueryAttempt[Employee]:
        """Read one bounded employee response and project selected identity fields."""
        if (
            not isinstance(input, GetEmployeeInput)
            or not isinstance(input.employee_id, str)
            or not _EMPLOYEE_UUID.fullmatch(input.employee_id)
        ):
            return _branch(
                "getEmployee", Employee(), "defect", FailureKind.VALIDATION, "Gusto employee UUID is invalid"
            )
        exchange = await self.client.get(call, f"/v1/employees/{input.employee_id}")
        failure = _read_failure(exchange, "getEmployee", Employee())
        if failure is not None:
            return failure
        employee = _decode_employee(_read_json(exchange.body))
        if employee is None or employee.employee_id.lower() != input.employee_id.lower():
            return _branch(
                "getEmployee",
                Employee(),
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Gusto returned invalid employee JSON",
            )
        return QueryAttempt.branch_result("found", employee, Receipt())


class ListEmployeesOperation:
    """Read a one-based page of employees from the authorized company."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated employee page contract."""
        return definitions.LIST_EMPLOYEES_DEFINITION

    async def invoke(self, call: Call, input: ListEmployeesInput) -> QueryAttempt[EmployeesPage]:
        """Return up to 100 employee identities from the requested company page."""
        if (
            not isinstance(input, ListEmployeesInput)
            or not isinstance(input.company_id, str)
            or not _EMPLOYEE_UUID.fullmatch(input.company_id)
            or type(input.page) is not int
            or not 1 <= input.page < 1 << 63
            or type(input.per_page) is not int
            or not 1 <= input.per_page <= 100
        ):
            return _branch(
                "listEmployees",
                EmployeesPage(),
                "defect",
                FailureKind.VALIDATION,
                "Gusto company UUID or page settings are invalid",
            )
        path = f"/v1/companies/{input.company_id}/employees?page={input.page}&per={input.per_page}"
        exchange = await self.client.get(call, path)
        failure = _read_failure(exchange, "listEmployees", EmployeesPage())
        if failure is not None:
            return failure
        page = _decode_employees_page(_read_json(exchange.body), input.company_id)
        if page is None:
            return _branch(
                "listEmployees",
                EmployeesPage(),
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Gusto returned invalid employee page JSON",
            )
        return QueryAttempt.branch_result("listed", page, Receipt())


def _decode_employees_page(document: object, company_id: str) -> EmployeesPage | None:
    if not isinstance(document, list) or len(document) > 100:
        return None
    employees = []
    for record in document:
        employee = _decode_employee(record)
        if employee is None or employee.company_id.lower() != company_id.lower():
            return None
        employees.append(employee)
    return EmployeesPage(tuple(employees))
