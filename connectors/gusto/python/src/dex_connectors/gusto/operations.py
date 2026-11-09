# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gusto getEmployee decisions matching the Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import Employee, GetEmployeeInput

_EMPLOYEE_UUID = re.compile(r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "gusto", "getEmployee", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[Employee]:
    return QueryAttempt.branch_result(branch, Employee(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Employee]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _read_failure(exchange: Exchange) -> QueryAttempt[Employee] | None:
    if exchange.error == "credential":
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "Gusto OAuth credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "Gusto request failed")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "Gusto rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(FailureKind.AVAILABILITY, "Gusto is temporarily unavailable", exchange.retry_after)
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Gusto response exceeded the configured limit"
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "Gusto response was interrupted")
    if exchange.status == 404:
        return QueryAttempt.branch_result("missing", Employee(), Receipt())
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"Gusto rejected HTTP {exchange.status}"
        )
    return None


def _decode_employee(body: bytes, input: GetEmployeeInput) -> Employee | None:
    try:
        record: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
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
        or employee_id.lower() != input.employee_id.lower()
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
        if not _EMPLOYEE_UUID.fullmatch(input.employee_id):
            return _branch("defect", FailureKind.VALIDATION, "Gusto employee UUID is invalid")
        exchange = await self.client.get(call, f"/v1/employees/{input.employee_id}")
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        employee = _decode_employee(exchange.body, input)
        if employee is None:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Gusto returned invalid employee JSON")
        return QueryAttempt.branch_result("found", employee, Receipt())
