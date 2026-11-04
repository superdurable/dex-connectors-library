# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""BambooHR operation invocations and provider-specific response decoding."""

import json
import math
import re
from datetime import UTC, datetime, timedelta
from email.utils import parseaddr
from typing import Any

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions
from .client import Client, Exchange, bamboohr_failure
from .models import (
    AddEmployeeInput,
    AddEmployeeOutput,
    EmployeeChange,
    EmployeeChangeCursor,
    EmployeeRecord,
    EmployeeSummary,
    FindEmployeeByEmailInput,
    FindEmployeeByEmailOutput,
    GetEmployeeInput,
    ListEmployeeChangesInput,
    ListEmployeeChangesOutput,
    ListTimeOffRequestsInput,
    ListTimeOffRequestsOutput,
    TimeOffDailyAmount,
    TimeOffRequest,
    UpdateEmployeeInput,
    UpdateEmployeeOutput,
)

_EMPLOYEE_ID = re.compile(r"^[1-9][0-9]{0,18}$")
_FIELD_NAME = re.compile(r"^[A-Za-z][A-Za-z0-9_]{0,63}$")
_FIELD_ID = re.compile(r"^[1-9][0-9]{0,9}$")
_ACTION = re.compile(r"^[A-Za-z]{1,32}$")
_LOCATION_ID = re.compile(r"(?:/employees/|[?&]id=)([1-9][0-9]{0,18})(?:[/?&#]|$)")
_SUMMARY_FIELDS = frozenset(
    (
        "employeeId",
        "firstName",
        "lastName",
        "preferredName",
        "jobTitleName",
        "status",
        "workEmail",
        "homeEmail",
        "photoUrl",
        "_restrictedFields",
    )
)
_HISTORY_FIELDS = frozenset(
    (
        "jobTitle",
        "department",
        "division",
        "location",
        "reportsTo",
        "payRate",
        "payType",
        "payPer",
        "paidPer",
        "paySchedule",
        "overtimeRate",
        "exempt",
        "employmentHistoryStatus",
        "employmentStatus",
        "employeeStatusDate",
        "employmentType",
        "terminationDate",
    )
)
_IGNORED_FIELDS = frozenset(("id", "employeeId", "photo", "photoUrl"))
_ADD_TYPED_FIELDS = frozenset(
    ("firstName", "lastName", "preferredName", "workEmail", "homeEmail", "hireDate")
)


def _valid_employee_id(value: str) -> bool:
    return isinstance(value, str) and _EMPLOYEE_ID.fullmatch(value) is not None


def _valid_email(value: str) -> bool:
    display, address = parseaddr(value)
    local, separator, domain = address.partition("@")
    return len(value) <= 254 and not display and address == value and bool(separator and local and domain)


def _valid_date(value: str) -> bool:
    try:
        return datetime.strptime(value, "%Y-%m-%d").strftime("%Y-%m-%d") == value
    except ValueError:
        return False


def _validate_fields(fields: tuple[str, ...], minimum: int, maximum: int, numeric: bool = True) -> None:
    if not minimum <= len(fields) <= maximum or len(fields) != len(set(fields)):
        raise ValueError("invalid BambooHR field selection")
    if any(
        not _FIELD_NAME.fullmatch(field) and (not numeric or not _FIELD_ID.fullmatch(field))
        for field in fields
    ):
        raise ValueError("invalid BambooHR field name")


def _validate_written_fields(fields: dict[str, str], minimum: int, refused: frozenset[str]) -> None:
    if not minimum <= len(fields) <= 50:
        raise ValueError("invalid BambooHR written field count")
    for name, value in fields.items():
        if not _FIELD_NAME.fullmatch(name) or name.lower() in {field.lower() for field in refused}:
            raise ValueError("BambooHR field cannot be written")
        if not isinstance(value, str) or len(value.encode("utf-8")) > 4096 or "\x00" in value:
            raise ValueError("invalid BambooHR field value")


def _read_json(body: bytes) -> Any:
    return json.loads(body, parse_constant=lambda value: _reject_non_json_number(value))


def _reject_non_json_number(value: str) -> None:
    raise ValueError(f"invalid JSON number: {value}")


def _go_int64(value: Any) -> int:
    if type(value) is not int or not -(1 << 63) <= value < 1 << 63:
        raise ValueError("integer is outside Go's int64 range")
    return value


def _go_float(value: Any) -> float | int:
    if type(value) not in (int, float):
        raise ValueError("amount is not a JSON number")
    try:
        number = float(value)
    except OverflowError as error:
        raise ValueError("amount is outside Go's float64 range") from error
    if not math.isfinite(number) or (type(value) is int and int(number) != value):
        raise ValueError("amount cannot be represented without loss")
    return int(number) if number.is_integer() and abs(number) < 1e21 else number


def _employee_id(value: Any) -> str:
    if type(value) is int:
        value = str(value)
    if not isinstance(value, str) or not _valid_employee_id(value):
        raise ValueError("the employee ID is not a positive integer")
    return value


def _field_text(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (dict, list)):
        return json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    return str(value)


def _employee_object(document: Any, id_key: str) -> tuple[str, dict[str, str]]:
    if not isinstance(document, dict):
        raise ValueError("the employee is not a JSON object")
    identifier = _employee_id(document.get(id_key))
    return identifier, {name: _field_text(value) for name, value in document.items() if name != id_key}


def _employee_record(body: bytes, expected_id: str, fields: tuple[str, ...]) -> EmployeeRecord:
    identifier, values = _employee_object(_read_json(body), "id")
    if identifier != expected_id:
        raise ValueError("the returned employee ID is not the requested one")
    return EmployeeRecord(identifier, values, tuple(sorted(name for name in fields if name not in values)))


def _query_failure(exchange: Exchange, empty: Any, receipt: Receipt) -> QueryAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    if exchange.outcome in ("rate_limited", "not_sent", "unavailable"):
        assert exchange.failure is not None
        return QueryAttempt.retry(exchange.failure, exchange.retry_after)
    branch = {"not_found": "notFound", "invalid": "invalidResponse", "defect": "defect"}.get(
        exchange.outcome, "providerRejected"
    )
    return QueryAttempt.branch_result(branch, empty, receipt, exchange.failure)


class GetEmployeeOperation:
    """Read one employee and report fields omitted by BambooHR."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-derived getEmployee definition."""
        return definitions.GET_EMPLOYEE_DEFINITION

    async def invoke(self, call: Call, input: GetEmployeeInput) -> QueryAttempt[EmployeeRecord]:
        """Invoke BambooHR Get Employee once and classify its outcome."""
        empty = EmployeeRecord(fields={})
        try:
            if not _valid_employee_id(input.employee_id):
                raise ValueError("invalid employeeId")
            _validate_fields(input.fields, 1, 400)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                bamboohr_failure("getEmployee", FailureKind.VALIDATION, str(error)),
            )
        credentials, failure = await self.client.resolve_credentials(call, "getEmployee")
        if failure is not None:
            return QueryAttempt.branch_result("defect", empty, Receipt(), failure)
        assert credentials is not None
        query = {"fields": ",".join(input.fields)}
        if input.include_future_values:
            query["onlyCurrent"] = "false"
        exchange = await self.client.exchange(
            call, credentials, "getEmployee", "GET", f"/employees/{input.employee_id}", query
        )
        receipt = self.client.receipt(call, input.employee_id)
        failure_attempt = _query_failure(exchange, empty, receipt)
        if failure_attempt is not None:
            return failure_attempt
        try:
            return QueryAttempt.branch_result(
                "found", _employee_record(exchange.body, input.employee_id, input.fields), receipt
            )
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                bamboohr_failure(
                    "getEmployee", FailureKind.PROTOCOL, "BambooHR returned an invalid employee"
                ),
            )


class FindEmployeeByEmailOperation:
    """Find one exact employee email in BambooHR's substring candidate page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-derived email search definition."""
        return definitions.FIND_EMPLOYEE_BY_EMAIL_DEFINITION

    async def invoke(
        self, call: Call, input: FindEmployeeByEmailInput
    ) -> QueryAttempt[FindEmployeeByEmailOutput]:
        """Invoke List Employees and classify exact, absent, or ambiguous matches."""
        empty = FindEmployeeByEmailOutput()
        email_field = input.email_field or "workEmail"
        try:
            if (
                not _valid_email(input.email)
                or email_field not in ("workEmail", "homeEmail")
                or (input.status or "") not in ("", "active", "inactive")
            ):
                raise ValueError("invalid BambooHR email filter")
            _validate_fields(input.fields, 0, 20, False)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                bamboohr_failure("findEmployeeByEmail", FailureKind.VALIDATION, str(error)),
            )
        credentials, failure = await self.client.resolve_credentials(call, "findEmployeeByEmail")
        if failure is not None:
            return QueryAttempt.branch_result("defect", empty, Receipt(), failure)
        assert credentials is not None
        fields = ["workEmail", "homeEmail"]
        fields.extend(field for field in input.fields if field not in fields and field not in _SUMMARY_FIELDS)
        query = {
            f"filter[{email_field}]": input.email,
            "fields": ",".join(fields),
            "sort": "employeeId",
            "page[limit]": "100",
        }
        if input.status:
            query["filter[status]"] = input.status
        exchange = await self.client.exchange(
            call, credentials, "findEmployeeByEmail", "GET", "/employees", query
        )
        receipt = self.client.receipt(call)
        failed = _query_failure(exchange, empty, receipt)
        if failed is not None:
            return failed
        try:
            document = _read_json(exchange.body)
            rows, meta = document["data"], document["meta"]
            total = int(meta["total"])
            if not isinstance(rows, list) or len(rows) > 100 or total < len(rows):
                raise ValueError("the page size or total is inconsistent")
            candidates = tuple(_employee_summary(row, input.fields) for row in rows)
            has_more = (
                bool(meta.get("page", {}).get("nextCursor"))
                or document.get("_links", {}).get("next") is not None
                or total > len(rows)
            )
        except (ValueError, TypeError, KeyError, AttributeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                bamboohr_failure(
                    "findEmployeeByEmail",
                    FailureKind.PROTOCOL,
                    "BambooHR returned an invalid employee list",
                ),
            )
        matches = tuple(
            candidate
            for candidate in candidates
            if getattr(candidate, "work_email" if email_field == "workEmail" else "home_email")
            .strip()
            .casefold()
            == input.email.casefold()
        )
        if len(matches) == 1 and not has_more:
            return QueryAttempt.branch_result(
                "found",
                FindEmployeeByEmailOutput(matches[0], candidate_count=total),
                self.client.receipt(call, matches[0].employee_id),
            )
        if not matches and not has_more:
            failure = bamboohr_failure(
                "findEmployeeByEmail",
                FailureKind.NOT_FOUND,
                f"no employee visible to the connection has this {email_field}",
            )
            return QueryAttempt.branch_result(
                "notFound", FindEmployeeByEmailOutput(candidate_count=total), receipt, failure
            )
        failure = bamboohr_failure(
            "findEmployeeByEmail",
            FailureKind.CONFLICT,
            f"exact matches: {len(matches)} of {total} employees whose {email_field} "
            "contains the address; no single employee was chosen",
        )
        return QueryAttempt.branch_result(
            "ambiguous", FindEmployeeByEmailOutput(matches=matches, candidate_count=total), receipt, failure
        )


def _employee_summary(document: Any, requested: tuple[str, ...]) -> EmployeeSummary:
    if not isinstance(document, dict):
        raise ValueError("employee is not an object")
    restricted = document.get("_restrictedFields") or []
    if not isinstance(restricted, list) or any(not isinstance(item, str) for item in restricted):
        raise ValueError("restricted fields are invalid")
    identifier, values = _employee_object(document, "employeeId")
    return EmployeeSummary(
        employee_id=identifier,
        first_name=values.get("firstName", ""),
        last_name=values.get("lastName", ""),
        preferred_name=values.get("preferredName", ""),
        job_title_name=values.get("jobTitleName", ""),
        status=values.get("status", ""),
        work_email=values.get("workEmail", ""),
        home_email=values.get("homeEmail", ""),
        fields={name: values[name] for name in requested if name in values and name not in _SUMMARY_FIELDS},
        restricted_fields=tuple(restricted),
    )


class ListEmployeeChangesOperation:
    """Poll and page BambooHR's changed employee map."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-derived employee changes definition."""
        return definitions.LIST_EMPLOYEE_CHANGES_DEFINITION

    async def invoke(
        self, call: Call, input: ListEmployeeChangesInput
    ) -> QueryAttempt[ListEmployeeChangesOutput]:
        """Return changes after the cursor in timestamp and employee ID order."""
        empty = ListEmployeeChangesOutput()
        limit = input.limit or 100
        try:
            if (
                input.since.year == 1
                or input.since.tzinfo is None
                or limit < 1
                or limit > 1000
                or (input.after_employee_id and not _valid_employee_id(input.after_employee_id))
                or (input.change_type or "") not in ("", "inserted", "updated", "deleted")
            ):
                raise ValueError("invalid BambooHR change cursor or filter")
        except (ValueError, TypeError) as error:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                bamboohr_failure("listEmployeeChanges", FailureKind.VALIDATION, str(error)),
            )
        credentials, failure = await self.client.resolve_credentials(call, "listEmployeeChanges")
        if failure is not None:
            return QueryAttempt.branch_result("defect", empty, Receipt(), failure)
        assert credentials is not None
        since = input.since.astimezone(UTC)
        query_instant = (
            (since - timedelta(seconds=1)).replace(microsecond=0).isoformat().replace("+00:00", "Z")
        )
        query = {"since": query_instant}
        if input.change_type:
            query["type"] = input.change_type
        exchange = await self.client.exchange(
            call, credentials, "listEmployeeChanges", "GET", "/employees/changed", query
        )
        receipt = self.client.receipt(call)
        failed = _query_failure(exchange, empty, receipt)
        if failed is not None:
            return failed
        try:
            document = _read_json(exchange.body)
            raw_changes = document["employees"]
            if raw_changes == []:
                raw_changes = {}
            if not isinstance(raw_changes, dict):
                raise ValueError("employees is not a map of changes")
            changes = []
            for identifier, item in raw_changes.items():
                _employee_id(identifier)
                if item.get("id") is not None and _employee_id(item["id"]) != identifier:
                    raise ValueError("change has mismatched ID")
                action = item["action"]
                if not isinstance(action, str) or not _ACTION.fullmatch(action):
                    raise ValueError("change has invalid action")
                last_changed = datetime.fromisoformat(item["lastChanged"].replace("Z", "+00:00")).astimezone(
                    UTC
                )
                changes.append(EmployeeChange(identifier, action=action, last_changed=last_changed))
            changes = [
                change
                for change in changes
                if change.last_changed > since
                or (
                    change.last_changed == since
                    and (
                        not input.after_employee_id or int(change.employee_id) > int(input.after_employee_id)
                    )
                )
            ]
            changes.sort(key=lambda change: (change.last_changed, int(change.employee_id)))
            selected = tuple(changes[:limit])
            cursor = (
                EmployeeChangeCursor(selected[-1].last_changed, selected[-1].employee_id)
                if selected
                else EmployeeChangeCursor(since, input.after_employee_id)
            )
            output = ListEmployeeChangesOutput(selected, cursor, len(changes) > limit)
        except (ValueError, TypeError, KeyError, AttributeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                bamboohr_failure(
                    "listEmployeeChanges",
                    FailureKind.PROTOCOL,
                    "BambooHR returned an invalid change list",
                ),
            )
        return QueryAttempt.branch_result("listed", output, receipt)


def _time_off_filter(input: ListTimeOffRequestsInput) -> str:
    clauses = [f"startDate le '{input.end_date}'", f"endDate ge '{input.start_date}'"]
    if input.employee_id:
        clauses.append(f"employeeId eq {input.employee_id}")
    if len(input.statuses) == 1:
        clauses.append(f"status eq '{input.statuses[0].lower()}'")
    elif input.statuses:
        statuses = ", ".join(f"'{status.lower()}'" for status in input.statuses)
        clauses.append(f"status in ({statuses})")
    return " and ".join(clauses)


def _time_off_request(row: Any) -> TimeOffRequest:
    if not isinstance(row, dict) or _go_int64(row["id"]) < 1 or _go_int64(row["employeeId"]) < 1:
        raise ValueError("the request or employee ID is missing")
    if not _valid_date(row["startDate"]) or not _valid_date(row["endDate"]):
        raise ValueError("a date is not YYYY-MM-DD")
    status, unit = row["status"].upper(), row["unit"].upper()
    if status not in ("REQUESTED", "APPROVED", "DENIED", "CANCELED") or unit not in ("HOURS", "DAYS"):
        raise ValueError("the status or unit is not one BambooHR documents")
    daily = tuple(
        TimeOffDailyAmount(item["date"], _go_float(item["amount"])) for item in row.get("dailyAmounts") or []
    )
    if any(not _valid_date(item.date) for item in daily):
        raise ValueError("a daily amount date is not YYYY-MM-DD")
    requested_at = datetime.fromisoformat(
        row.get("requestedAt", "0001-01-01T00:00:00Z").replace("Z", "+00:00")
    ).astimezone(UTC)
    updated = row.get("statusUpdatedAt")
    updated_at = datetime.fromisoformat(updated.replace("Z", "+00:00")).astimezone(UTC) if updated else None
    return TimeOffRequest(
        id=_go_int64(row["id"]),
        employee_id=str(_go_int64(row["employeeId"])),
        category_id=_go_int64(row.get("categoryId", 0)),
        start_date=row["startDate"],
        end_date=row["endDate"],
        amount=_go_float(row.get("amount", 0)),
        unit=unit,
        status=status,
        requested_at=requested_at,
        status_updated_at=updated_at,
        daily_amounts=daily,
    )


class ListTimeOffRequestsOperation:
    """Read one filtered page of BambooHR time off requests."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-derived time off definition."""
        return definitions.LIST_TIME_OFF_REQUESTS_DEFINITION

    async def invoke(
        self, call: Call, input: ListTimeOffRequestsInput
    ) -> QueryAttempt[ListTimeOffRequestsOutput]:
        """Invoke List Time Off Requests and decode its bounded page."""
        empty = ListTimeOffRequestsOutput()
        page, page_size = input.page or 1, input.page_size or 100
        try:
            if (
                not _valid_date(input.start_date)
                or not _valid_date(input.end_date)
                or input.end_date < input.start_date
                or (input.employee_id and not _valid_employee_id(input.employee_id))
                or any(
                    status not in ("REQUESTED", "APPROVED", "DENIED", "CANCELED") for status in input.statuses
                )
                or not 1 <= page <= 2_147_483_647
                or not 1 <= page_size <= 200
            ):
                raise ValueError("invalid BambooHR time off filter")
        except (ValueError, TypeError) as error:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                bamboohr_failure("listTimeOffRequests", FailureKind.VALIDATION, str(error)),
            )
        credentials, failure = await self.client.resolve_credentials(call, "listTimeOffRequests")
        if failure is not None:
            return QueryAttempt.branch_result("defect", empty, Receipt(), failure)
        assert credentials is not None
        query = {"filter": _time_off_filter(input), "page": str(page), "pageSize": str(page_size)}
        exchange = await self.client.exchange(
            call, credentials, "listTimeOffRequests", "GET", "/time-off/requests", query
        )
        receipt = self.client.receipt(call, input.employee_id)
        failed = _query_failure(exchange, empty, receipt)
        if failed is not None:
            return failed
        try:
            document = _read_json(exchange.body)
            rows, meta = document["data"], document["meta"]
            total_items = _go_int64(meta.get("totalItems", 0))
            total_pages = _go_int64(meta.get("totalPages", 0))
            if not isinstance(rows, list) or len(rows) > page_size or total_items < 0 or total_pages < 0:
                raise ValueError("the page size or totals are inconsistent")
            output = ListTimeOffRequestsOutput(
                tuple(_time_off_request(row) for row in rows),
                page,
                total_items,
                page + 1 if page < total_pages else 0,
            )
        except (ValueError, TypeError, KeyError, AttributeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                bamboohr_failure(
                    "listTimeOffRequests",
                    FailureKind.PROTOCOL,
                    "BambooHR returned an invalid time off page",
                ),
            )
        return QueryAttempt.branch_result("listed", output, receipt)


def _mutation_exchange(
    exchange: Exchange, value: UpdateEmployeeOutput, receipt: Receipt
) -> MutationAttempt[UpdateEmployeeOutput] | None:
    if exchange.outcome == "succeeded":
        return None
    if exchange.outcome in ("rate_limited", "not_sent", "unavailable"):
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    branch = {"not_found": "notFound", "invalid": "invalidResponse", "defect": "defect"}.get(
        exchange.outcome, "providerRejected"
    )
    return MutationAttempt.branch_result(branch, value, receipt, exchange.failure)


class UpdateEmployeeOperation:
    """Set absolute plain employee fields and read them back."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-derived update definition."""
        return definitions.UPDATE_EMPLOYEE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateEmployeeInput) -> IdempotencyKey:
        """Correlate the absolute replacement with the stable connector Call ID."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateEmployeeInput) -> MutationAttempt[UpdateEmployeeOutput]:
        """Read, write only differences, and read the resulting field values."""
        empty = UpdateEmployeeOutput(employee=EmployeeRecord(fields={}))
        try:
            if not _valid_employee_id(input.employee_id):
                raise ValueError("invalid employeeId")
            _validate_written_fields(input.fields or {}, 1, _HISTORY_FIELDS | _IGNORED_FIELDS)
        except (ValueError, TypeError) as error:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                bamboohr_failure("updateEmployee", FailureKind.VALIDATION, str(error)),
            )
        credentials, failure = await self.client.resolve_credentials(call, "updateEmployee")
        if failure is not None:
            return MutationAttempt.branch_result("defect", empty, Receipt(), failure)
        assert credentials is not None
        fields = input.fields or {}
        receipt = self.client.receipt(call, input.employee_id)
        names = tuple(sorted(fields))
        current_exchange = await self.client.exchange(
            call,
            credentials,
            "updateEmployee",
            "GET",
            f"/employees/{input.employee_id}",
            {"fields": ",".join(names)},
        )
        terminal = _mutation_exchange(current_exchange, empty, receipt)
        if terminal is not None:
            return terminal
        try:
            current = _employee_record(current_exchange.body, input.employee_id, names)
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                bamboohr_failure(
                    "updateEmployee", FailureKind.PROTOCOL, "BambooHR returned an invalid employee"
                ),
            )
        writes = {
            name: value
            for name, value in fields.items()
            if current.fields is None or current.fields.get(name) != value
        }
        if not writes:
            return MutationAttempt.branch_result(
                "updated", UpdateEmployeeOutput(employee=current, was_already_applied=True), receipt
            )
        write_exchange = await self.client.exchange(
            call, credentials, "updateEmployee", "POST", f"/employees/{input.employee_id}", payload=writes
        )
        terminal = _mutation_exchange(write_exchange, UpdateEmployeeOutput(employee=current), receipt)
        if terminal is not None:
            return terminal
        output = UpdateEmployeeOutput(
            employee=EmployeeRecord(fields={}), written_fields=tuple(sorted(writes))
        )
        stored_exchange = await self.client.exchange(
            call,
            credentials,
            "updateEmployee",
            "GET",
            f"/employees/{input.employee_id}",
            {"fields": ",".join(names)},
        )
        terminal = _mutation_exchange(stored_exchange, output, receipt)
        if terminal is not None:
            return terminal
        try:
            stored = _employee_record(stored_exchange.body, input.employee_id, names)
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                bamboohr_failure(
                    "updateEmployee", FailureKind.PROTOCOL, "BambooHR returned an invalid employee"
                ),
            )
        unmatched = tuple(
            name for name in names if stored.fields is None or stored.fields.get(name) != fields[name]
        )
        return MutationAttempt.branch_result(
            "updated",
            UpdateEmployeeOutput(stored, tuple(sorted(writes)), unmatched_fields=unmatched),
            receipt,
        )


class AddEmployeeOperation:
    """Create one employee using the SDK's durable single-dispatch fence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-derived fenced addEmployee definition."""
        return definitions.ADD_EMPLOYEE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddEmployeeInput) -> IdempotencyKey:
        """Correlate attempts while the fence controls provider dispatch."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AddEmployeeInput) -> MutationAttempt[AddEmployeeOutput]:
        """Send at most one BambooHR employee creation per logical call."""
        empty = AddEmployeeOutput()
        try:
            for name, value in (
                ("firstName", input.first_name),
                ("lastName", input.last_name),
                ("preferredName", input.preferred_name),
            ):
                if (
                    (name != "preferredName" and not value)
                    or len(value.encode()) > 255
                    or value != value.strip()
                    or any(ord(character) < 32 or ord(character) == 127 for character in value)
                ):
                    raise ValueError(f"invalid {name}")
            if any(value and not _valid_email(value) for value in (input.work_email, input.home_email)) or (
                input.hire_date and not _valid_date(input.hire_date)
            ):
                raise ValueError("invalid BambooHR email or hire date")
            _validate_written_fields(input.fields, 0, _ADD_TYPED_FIELDS | _IGNORED_FIELDS)
        except (ValueError, TypeError) as error:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                bamboohr_failure("addEmployee", FailureKind.VALIDATION, str(error)),
            )
        receipt = self.client.receipt(call)
        try:
            fence = Fence(call, definitions.ADD_EMPLOYEE_DEFINITION.step_defaults.execute_method_timeout)
            decision = await fence.decide()
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                bamboohr_failure("addEmployee", FailureKind.LOCAL_DEFECT, str(error)),
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return _add_uncertain(
                receipt,
                FailureKind.TRANSPORT,
                "the dispatch checkpoint is unavailable or untrusted; the request is not sent again",
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            identifier = decision.marker.provider_id if decision.marker else ""
            if not _valid_employee_id(identifier):
                return _add_uncertain(
                    receipt,
                    FailureKind.TRANSPORT,
                    "an earlier attempt may have sent the request; no employee ID is available for read-back",
                )
            return await self._read_back(call, identifier)
        credentials, failure = await self.client.resolve_credentials(call, "addEmployee")
        if failure is not None:
            try:
                await fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(
                    bamboohr_failure(
                        "addEmployee",
                        FailureKind.AVAILABILITY,
                        "credentials and not-sent checkpoint are unavailable",
                    )
                )
            return MutationAttempt.branch_result("defect", empty, receipt, failure)
        assert credentials is not None
        payload = dict(input.fields)
        payload.update(firstName=input.first_name, lastName=input.last_name)
        for key, value in (
            ("preferredName", input.preferred_name),
            ("workEmail", input.work_email),
            ("homeEmail", input.home_email),
            ("hireDate", input.hire_date),
        ):
            if value:
                payload[key] = value

        async def send(deadline: datetime) -> Exchange:
            return await self.client.exchange(
                call, credentials, "addEmployee", "POST", "/employees", payload=payload
            )

        try:
            exchange = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                bamboohr_failure(
                    "addEmployee",
                    FailureKind.AVAILABILITY,
                    "dispatch checkpoint or attempt time is unavailable; nothing was sent",
                )
            )
        except Exception:
            return _add_uncertain(receipt, FailureKind.TRANSPORT, "the dispatch outcome is unavailable")
        if 200 <= exchange.status_code < 300:
            try:
                identifier = _employee_id(_read_json(exchange.body)["id"])
            except (ValueError, TypeError, KeyError):
                match = _LOCATION_ID.search(exchange.headers.get("Location", ""))
                identifier = match.group(1) if match else ""
            if not identifier:
                return _add_uncertain(
                    receipt,
                    FailureKind.PROTOCOL,
                    "BambooHR accepted the employee but returned no usable employee ID",
                )
            try:
                await fence.record_provider_id(identifier)
            except Exception:
                pass
            return MutationAttempt.branch_result(
                "created", AddEmployeeOutput(identifier), self.client.receipt(call, identifier)
            )
        if exchange.status_code in (429, 503) or exchange.outcome == "not_sent":
            try:
                await fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    bamboohr_failure(
                        "addEmployee",
                        FailureKind.AVAILABILITY,
                        "BambooHR did not process the request, but the resend checkpoint failed",
                    ),
                    exchange.retry_after,
                )
            assert exchange.failure is not None
            return MutationAttempt.retry(exchange.failure, exchange.retry_after)
        if exchange.outcome in ("unavailable", "invalid"):
            assert exchange.failure is not None
            return MutationAttempt.uncertain(empty, exchange.failure, receipt)
        if exchange.outcome == "defect":
            return MutationAttempt.branch_result("defect", empty, receipt, exchange.failure)
        return MutationAttempt.branch_result("providerRejected", empty, receipt, exchange.failure)

    async def _read_back(self, call: Call, identifier: str) -> MutationAttempt[AddEmployeeOutput]:
        receipt = self.client.receipt(call, identifier)
        credentials, failure = await self.client.resolve_credentials(call, "addEmployee")
        if failure is not None:
            return MutationAttempt.retry(failure)
        assert credentials is not None
        exchange = await self.client.exchange(
            call, credentials, "addEmployee", "GET", f"/employees/{identifier}"
        )
        if exchange.outcome == "succeeded":
            try:
                if _employee_id(_read_json(exchange.body)["id"]) == identifier:
                    return MutationAttempt.branch_result("created", AddEmployeeOutput(identifier), receipt)
            except (ValueError, TypeError, KeyError):
                pass
            return _add_uncertain(
                receipt, FailureKind.PROTOCOL, "the created employee ID could not be confirmed"
            )
        if exchange.outcome in ("not_found", "rejected"):
            assert exchange.failure is not None
            return _add_uncertain(
                receipt, exchange.failure.kind, "the created employee could not be confirmed"
            )
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)


def _add_uncertain(receipt: Receipt, kind: FailureKind, message: str) -> MutationAttempt[AddEmployeeOutput]:
    return MutationAttempt.uncertain(
        AddEmployeeOutput(), bamboohr_failure("addEmployee", kind, message), receipt
    )
