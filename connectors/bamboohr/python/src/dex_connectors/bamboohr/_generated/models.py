# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# GetEmployeeInput names one employee and the fields to read.
@dataclass(frozen=True, slots=True)
class GetEmployeeInput:
    # EmployeeID is BambooHR's internal employee ID, such as 123. The editable Employee # and the
    # caller sentinel 0 are not accepted.
    employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'employeeId'})
    # Fields lists 1 to 400 fields to read: standard names such as workEmail, custom-field
    # aliases such as customStartDate, or numeric field IDs from BambooHR's List Fields endpoint.
    # BambooHR returns nothing but the ID when no field is requested, so at least one is required.
    fields: tuple[str, ...] = ()
    # IncludeFutureValues also returns future-dated values from history tables such as job
    # information, BambooHR's onlyCurrent=false. False reads only currently effective values.
    include_future_values: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includeFutureValues', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'fields', _replace_nil_slices(self.fields, 1))


GetEmployeeInput.__annotations__['fields'] = tuple[str, ...] | None
GetEmployeeInput.__dataclass_fields__['fields'].type = tuple[str, ...] | None
setattr(GetEmployeeInput, '__module__', 'dex_connectors.bamboohr.models')


# EmployeeRecord is one employee's requested fields as BambooHR returned them.
@dataclass(frozen=True, slots=True)
class EmployeeRecord:
    # EmployeeID is BambooHR's immutable internal employee ID, a string of digits such as 123.
    # It is not the editable Employee # (employeeNumber).
    employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'employeeId'})
    # Fields maps each returned field to its value. BambooHR returns most values as text; a null
    # value is empty, a boolean is true or false, and a structured value is its compact JSON.
    # A field requested by numeric ID may be returned under its name.
    fields: dict[str, str] | None = None
    # OmittedFields lists requested fields BambooHR did not return, sorted. BambooHR silently
    # omits a field that is unknown or that the connection's user cannot view.
    omitted_fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'omittedFields', 'dex_json_omit_default': True})


setattr(EmployeeRecord, '__module__', 'dex_connectors.bamboohr.models')


# FindEmployeeByEmailInput is one email lookup.
@dataclass(frozen=True, slots=True)
class FindEmployeeByEmailInput:
    # Email is one bare address such as ava.nguyen@example.com. Letter case is ignored.
    email: str = ''
    # EmailField is the field to match, workEmail or homeEmail; blank matches workEmail.
    email_field: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'emailField', 'dex_json_omit_default': True})
    # Status keeps only active or inactive employees; blank keeps both.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Fields lists up to 20 additional List Employees field names to return per employee, such as
    # hireDate or departmentName. BambooHR ignores an unknown name.
    fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(FindEmployeeByEmailInput, '__module__', 'dex_connectors.bamboohr.models')


# EmployeeSummary is one employee as BambooHR's List Employees endpoint returned it.
@dataclass(frozen=True, slots=True)
class EmployeeSummary:
    # EmployeeID is BambooHR's internal employee ID.
    employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'employeeId'})
    # FirstName is the legal first name.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName is the legal last name.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})
    # PreferredName is the preferred name, or empty.
    preferred_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'preferredName', 'dex_json_omit_default': True})
    # JobTitleName is the current job title.
    job_title_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'jobTitleName', 'dex_json_omit_default': True})
    # Status is BambooHR's employee status, Active or Inactive.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # WorkEmail is the work email address, or empty.
    work_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'workEmail', 'dex_json_omit_default': True})
    # HomeEmail is the home email address, or empty.
    home_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'homeEmail', 'dex_json_omit_default': True})
    # Fields holds the additional requested fields BambooHR returned, as text.
    fields: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # RestrictedFields lists fields BambooHR returned as null because the connection cannot read them.
    restricted_fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'restrictedFields', 'dex_json_omit_default': True})


setattr(EmployeeSummary, '__module__', 'dex_connectors.bamboohr.models')


# FindEmployeeByEmailOutput is the result of one email lookup.
@dataclass(frozen=True, slots=True)
class FindEmployeeByEmailOutput:
    # Employee is the one matching employee on the found branch.
    employee: EmployeeSummary = _dataclass_field(default_factory=EmployeeSummary)
    # Matches lists every exact match read on the ambiguous branch, which may be fewer than two
    # when more than MaxEmailCandidates employee emails contain the address.
    matches: tuple[EmployeeSummary, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # CandidateCount is BambooHR's count of employees whose email contains the address.
    candidate_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'candidateCount'})


setattr(FindEmployeeByEmailOutput, '__module__', 'dex_connectors.bamboohr.models')


# ListEmployeeChangesInput is one poll of BambooHR's employee change history.
@dataclass(frozen=True, slots=True)
class ListEmployeeChangesInput:
    # Since lists changes at or after this instant. It is required; store the previous call's
    # NextCursor and pass it back to continue.
    since: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_format': 'rfc3339'})
    # AfterEmployeeID skips changes at exactly Since for this employee ID and every lower ID, as
    # NextCursor sets it. Empty includes every change at Since.
    after_employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'afterEmployeeId', 'dex_json_omit_default': True})
    # ChangeType keeps one change type; blank lists inserted, updated, and deleted employees.
    change_type: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'changeType', 'dex_json_omit_default': True})
    # Limit is the most changes to return, 1 to 1000; zero uses DefaultEmployeeChangeLimit.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListEmployeeChangesInput, '__module__', 'dex_connectors.bamboohr.models')


# EmployeeChange is one changed employee. BambooHR reports only each employee's latest change.
@dataclass(frozen=True, slots=True)
class EmployeeChange:
    # EmployeeID is BambooHR's internal employee ID.
    employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'employeeId'})
    # Action is BambooHR's change action, such as Inserted.
    action: str = _dataclass_field(kw_only=True)
    # LastChanged is when the employee last changed, in UTC.
    last_changed: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'lastChanged', 'dex_json_format': 'rfc3339'})


setattr(EmployeeChange, '__module__', 'dex_connectors.bamboohr.models')


# EmployeeChangeCursor is a position in BambooHR's change history: changes after LastChanged,
# or at LastChanged for an employee ID above AfterEmployeeID.
@dataclass(frozen=True, slots=True)
class EmployeeChangeCursor:
    # Since is the change instant the cursor stands at, in UTC.
    since: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_format': 'rfc3339'})
    # AfterEmployeeID is the last employee ID already returned at Since; empty includes every
    # change at Since.
    after_employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'afterEmployeeId', 'dex_json_omit_default': True})


setattr(EmployeeChangeCursor, '__module__', 'dex_connectors.bamboohr.models')


# ListEmployeeChangesOutput is one page of changes, oldest first.
@dataclass(frozen=True, slots=True)
class ListEmployeeChangesOutput:
    # Changes lists the changes after the cursor, ordered by LastChanged and then employee ID.
    changes: tuple[EmployeeChange, ...] = ()
    # NextCursor is where the next poll starts: after the last returned change, or the input
    # cursor when nothing changed.
    next_cursor: EmployeeChangeCursor = _dataclass_field(default_factory=EmployeeChangeCursor, metadata={'dex_json_name': 'nextCursor'})
    # HasMore reports that more changes than Limit exist after the cursor, so poll again now.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'changes', _replace_nil_slices(self.changes, 1))


ListEmployeeChangesOutput.__annotations__['changes'] = tuple[EmployeeChange, ...] | None
ListEmployeeChangesOutput.__dataclass_fields__['changes'].type = tuple[EmployeeChange, ...] | None
setattr(ListEmployeeChangesOutput, '__module__', 'dex_connectors.bamboohr.models')


# ListTimeOffRequestsInput selects one page of time off requests that overlap a date window.
@dataclass(frozen=True, slots=True)
class ListTimeOffRequestsInput:
    # StartDate is the first day of the window, written YYYY-MM-DD. It is required.
    start_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'startDate'})
    # EndDate is the last day of the window, written YYYY-MM-DD, on or after StartDate. It is required.
    end_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'endDate'})
    # EmployeeID keeps one employee's requests; empty lists every employee the connection can view.
    employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'employeeId', 'dex_json_omit_default': True})
    # Statuses keeps requests in any of these statuses; empty keeps every status.
    statuses: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Page is the 1-based page to read; zero reads page 1.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PageSize is 1 to 200 requests per page; zero uses DefaultTimeOffPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})


setattr(ListTimeOffRequestsInput, '__module__', 'dex_connectors.bamboohr.models')


# TimeOffDailyAmount is the amount of a request that falls on one day.
@dataclass(frozen=True, slots=True)
class TimeOffDailyAmount:
    # Date is the day, written YYYY-MM-DD.
    date: str = ''
    # Amount is the time requested on that day, in the request's Unit.
    amount: float = 0.0


setattr(TimeOffDailyAmount, '__module__', 'dex_connectors.bamboohr.models')


# TimeOffRequest is one time off request. Employee and manager notes are not returned, so free
# text about an absence never enters Step state.
@dataclass(frozen=True, slots=True)
class TimeOffRequest:
    # ID is BambooHR's time off request ID.
    id: int = 0
    # EmployeeID is the internal ID of the employee who requested the time off.
    employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'employeeId'})
    # CategoryID is BambooHR's time off category (type) ID.
    category_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'categoryId'})
    # StartDate is the first day off, written YYYY-MM-DD.
    start_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'startDate'})
    # EndDate is the last day off, written YYYY-MM-DD.
    end_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'endDate'})
    # Amount is the time requested, measured in Unit.
    amount: float = 0.0
    # Unit is HOURS or DAYS.
    unit: str = _dataclass_field(kw_only=True)
    # Status is BambooHR's request status.
    status: str = _dataclass_field(kw_only=True)
    # RequestedAt is when the request was made, in UTC.
    requested_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'requestedAt', 'dex_json_format': 'rfc3339'})
    # StatusUpdatedAt is when the status last changed, or nil when it never changed.
    status_updated_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'statusUpdatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # DailyAmounts is the amount requested on each day, in Unit.
    daily_amounts: tuple[TimeOffDailyAmount, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'dailyAmounts', 'dex_json_omit_default': True})


setattr(TimeOffRequest, '__module__', 'dex_connectors.bamboohr.models')


# ListTimeOffRequestsOutput is one page of time off requests.
@dataclass(frozen=True, slots=True)
class ListTimeOffRequestsOutput:
    # Requests holds the page's requests in BambooHR's default order, newest request first.
    requests: tuple[TimeOffRequest, ...] = ()
    # Page is the page read.
    page: int = 0
    # TotalItems is BambooHR's count of matching requests across all pages.
    total_items: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalItems'})
    # NextPage is the page to read next, or zero after the last page.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'requests', _replace_nil_slices(self.requests, 1))


ListTimeOffRequestsOutput.__annotations__['requests'] = tuple[TimeOffRequest, ...] | None
ListTimeOffRequestsOutput.__dataclass_fields__['requests'].type = tuple[TimeOffRequest, ...] | None
setattr(ListTimeOffRequestsOutput, '__module__', 'dex_connectors.bamboohr.models')


# UpdateEmployeeInput sets selected fields of one employee.
@dataclass(frozen=True, slots=True)
class UpdateEmployeeInput:
    # EmployeeID is BambooHR's internal employee ID, such as 123.
    employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'employeeId'})
    # Fields maps 1 to 50 field names or custom-field aliases, such as workEmail or
    # customITProvisioning, to the text to store; an empty value clears the field. Values use
    # BambooHR's field formats, such as YYYY-MM-DD for a date. Fields BambooHR keeps in its job
    # information, compensation, or employment status history tables, such as jobTitle,
    # department, location, payRate, employmentHistoryStatus, and terminationDate, are refused,
    # because writing them can append a history row and a repeated attempt could append another.
    # Numeric field IDs are refused for the same reason.
    fields: dict[str, str] | None = None


setattr(UpdateEmployeeInput, '__module__', 'dex_connectors.bamboohr.models')


# UpdateEmployeeOutput is the employee after the change.
@dataclass(frozen=True, slots=True)
class UpdateEmployeeOutput:
    # Employee holds the requested fields as read back after the write, or as read when every
    # value was already applied.
    employee: EmployeeRecord = _dataclass_field(default_factory=EmployeeRecord)
    # WrittenFields lists the fields this attempt sent, sorted; the others already held their value.
    written_fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'writtenFields', 'dex_json_omit_default': True})
    # WasAlreadyApplied reports that the employee already held every requested value, for example
    # because an earlier attempt of this Step wrote them, so this attempt wrote nothing.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})
    # UnmatchedFields lists requested fields whose stored value differs from the requested text
    # after the write, sorted: BambooHR rewrites some values, such as a state name to its
    # abbreviation, and silently drops a field the connection cannot see or edit.
    unmatched_fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'unmatchedFields', 'dex_json_omit_default': True})


setattr(UpdateEmployeeOutput, '__module__', 'dex_connectors.bamboohr.models')


# AddEmployeeInput is one new employee. BambooHR requires only the first and last name.
@dataclass(frozen=True, slots=True)
class AddEmployeeInput:
    # FirstName is the legal first name. It is required.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the legal last name. It is required.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})
    # PreferredName is the preferred name, or empty for none.
    preferred_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'preferredName', 'dex_json_omit_default': True})
    # WorkEmail is a bare work email address, or empty when IT has not issued one yet. BambooHR
    # rejects an email another employee already holds.
    work_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'workEmail', 'dex_json_omit_default': True})
    # HomeEmail is a bare personal email address, or empty.
    home_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'homeEmail', 'dex_json_omit_default': True})
    # HireDate is the hire date written YYYY-MM-DD, or empty.
    hire_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'hireDate', 'dex_json_omit_default': True})
    # Fields maps up to 50 other writable field names or custom-field aliases to their text, such
    # as department or employmentHistoryStatus, which sets the first employment status record.
    # It cannot repeat a field above, name a numeric field ID, or set id or a photo.
    fields: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(AddEmployeeInput, '__module__', 'dex_connectors.bamboohr.models')


# AddEmployeeOutput is the employee BambooHR added.
@dataclass(frozen=True, slots=True)
class AddEmployeeOutput:
    # EmployeeID is the new employee's internal employee ID.
    employee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'employeeId'})


setattr(AddEmployeeOutput, '__module__', 'dex_connectors.bamboohr.models')
