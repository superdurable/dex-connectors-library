# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddEmployeeInput, AddEmployeeOutput, EmployeeChange, EmployeeChangeCursor, EmployeeRecord, EmployeeSummary, FindEmployeeByEmailInput, FindEmployeeByEmailOutput, GetEmployeeInput, ListEmployeeChangesInput, ListEmployeeChangesOutput, ListTimeOffRequestsInput, ListTimeOffRequestsOutput, TimeOffDailyAmount, TimeOffRequest, UpdateEmployeeInput, UpdateEmployeeOutput
from .definitions import CONNECTOR_ID, ADD_EMPLOYEE_DEFINITION, AMBIGUOUS, CREATED, DEFECT, FIND_EMPLOYEE_BY_EMAIL_DEFINITION, FOUND, GET_EMPLOYEE_DEFINITION, INVALID_RESPONSE, LISTED, LIST_EMPLOYEE_CHANGES_DEFINITION, LIST_TIME_OFF_REQUESTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UNCERTAIN, UPDATED, UPDATE_EMPLOYEE_DEFINITION

def get_employee_found(value: EmployeeRecord, failure: Failure | None = None) -> Case[EmployeeRecord]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_employee_found, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def get_employee_not_found(value: EmployeeRecord, failure: Failure | None = None) -> Case[EmployeeRecord]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_employee_not_found, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def get_employee_provider_rejected(value: EmployeeRecord, failure: Failure | None = None) -> Case[EmployeeRecord]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_employee_provider_rejected, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def get_employee_invalid_response(value: EmployeeRecord, failure: Failure | None = None) -> Case[EmployeeRecord]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_employee_invalid_response, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def get_employee_defect(value: EmployeeRecord, failure: Failure | None = None) -> Case[EmployeeRecord]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_employee_defect, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def get_employee_retry(failure: Failure, after_seconds: float = 0) -> Case[EmployeeRecord]:
    return Case.retry(failure, after_seconds)

setattr(get_employee_retry, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def get_employee_found_default_answer() -> Case[EmployeeRecord]:
    return Case.branch_result(FOUND, EmployeeRecord(employee_id='140', fields={'firstName': 'Ava', 'lastName': 'Nguyen', 'department': 'Engineering'}))

setattr(get_employee_found_default_answer, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def get_employee_not_found_failure() -> Case[EmployeeRecord]:
    return Case.branch_result(NOT_FOUND, EmployeeRecord(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getEmployee", "No employee has this internal ID."))

setattr(get_employee_not_found_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def get_employee_provider_rejected_failure() -> Case[EmployeeRecord]:
    return Case.branch_result(PROVIDER_REJECTED, EmployeeRecord(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getEmployee", "The API user cannot view this employee."))

setattr(get_employee_provider_rejected_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def get_employee_invalid_response_failure() -> Case[EmployeeRecord]:
    return Case.branch_result(INVALID_RESPONSE, EmployeeRecord(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getEmployee", "BambooHR returned a malformed employee record."))

setattr(get_employee_invalid_response_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_found(value: FindEmployeeByEmailOutput, failure: Failure | None = None) -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_employee_by_email_found, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_not_found(value: FindEmployeeByEmailOutput, failure: Failure | None = None) -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_employee_by_email_not_found, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_ambiguous(value: FindEmployeeByEmailOutput, failure: Failure | None = None) -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(AMBIGUOUS, value, failure)

setattr(find_employee_by_email_ambiguous, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_provider_rejected(value: FindEmployeeByEmailOutput, failure: Failure | None = None) -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_employee_by_email_provider_rejected, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_invalid_response(value: FindEmployeeByEmailOutput, failure: Failure | None = None) -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_employee_by_email_invalid_response, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_defect(value: FindEmployeeByEmailOutput, failure: Failure | None = None) -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_employee_by_email_defect, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[FindEmployeeByEmailOutput]:
    return Case.retry(failure, after_seconds)

setattr(find_employee_by_email_retry, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_found_default_answer() -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(FOUND, FindEmployeeByEmailOutput(employee=EmployeeSummary(employee_id='140', first_name='Ava', last_name='Nguyen', status='Active', work_email='ava.nguyen@example.com'), candidate_count=1))

setattr(find_employee_by_email_found_default_answer, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_not_found_failure() -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(NOT_FOUND, FindEmployeeByEmailOutput(employee=EmployeeSummary()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "findEmployeeByEmail", "No employee visible to the connection has this email; BambooHR drops employees whose email the connection cannot read."))

setattr(find_employee_by_email_not_found_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_ambiguous_failure() -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(AMBIGUOUS, FindEmployeeByEmailOutput(employee=EmployeeSummary(), matches=(EmployeeSummary(employee_id='150', first_name='Alex', last_name='Chen', status='Active', work_email='shared@example.com'), EmployeeSummary(employee_id='151', first_name='Robin', last_name='Lee', status='Active', work_email='shared@example.com')), candidate_count=2), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "findEmployeeByEmail", "Two employees have this work email; no single employee was chosen."))

setattr(find_employee_by_email_ambiguous_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_provider_rejected_failure() -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(PROVIDER_REJECTED, FindEmployeeByEmailOutput(employee=EmployeeSummary()), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "findEmployeeByEmail", "The API key is invalid."))

setattr(find_employee_by_email_provider_rejected_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def find_employee_by_email_invalid_response_failure() -> Case[FindEmployeeByEmailOutput]:
    return Case.branch_result(INVALID_RESPONSE, FindEmployeeByEmailOutput(employee=EmployeeSummary()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "findEmployeeByEmail", "BambooHR returned a malformed employee list."))

setattr(find_employee_by_email_invalid_response_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_employee_changes_listed(value: ListEmployeeChangesOutput, failure: Failure | None = None) -> Case[ListEmployeeChangesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_employee_changes_listed, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_employee_changes_provider_rejected(value: ListEmployeeChangesOutput, failure: Failure | None = None) -> Case[ListEmployeeChangesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_employee_changes_provider_rejected, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_employee_changes_invalid_response(value: ListEmployeeChangesOutput, failure: Failure | None = None) -> Case[ListEmployeeChangesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_employee_changes_invalid_response, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_employee_changes_defect(value: ListEmployeeChangesOutput, failure: Failure | None = None) -> Case[ListEmployeeChangesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_employee_changes_defect, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_employee_changes_retry(failure: Failure, after_seconds: float = 0) -> Case[ListEmployeeChangesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_employee_changes_retry, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_employee_changes_listed_default_answer() -> Case[ListEmployeeChangesOutput]:
    return Case.branch_result(LISTED, ListEmployeeChangesOutput(changes=(EmployeeChange(employee_id='140', action='Updated', last_changed=datetime.fromisoformat("2026-10-13T10:00:00Z".replace('Z', '+00:00'))), EmployeeChange(employee_id='141', action='Updated', last_changed=datetime.fromisoformat("2026-10-13T11:00:00Z".replace('Z', '+00:00')))), next_cursor=EmployeeChangeCursor(since=datetime.fromisoformat("2026-10-13T11:00:00Z".replace('Z', '+00:00')), after_employee_id='141'), has_more=False))

setattr(list_employee_changes_listed_default_answer, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_employee_changes_provider_rejected_failure() -> Case[ListEmployeeChangesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListEmployeeChangesOutput(next_cursor=EmployeeChangeCursor()), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listEmployeeChanges", "The API key is invalid."))

setattr(list_employee_changes_provider_rejected_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_employee_changes_invalid_response_failure() -> Case[ListEmployeeChangesOutput]:
    return Case.branch_result(INVALID_RESPONSE, ListEmployeeChangesOutput(next_cursor=EmployeeChangeCursor()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listEmployeeChanges", "BambooHR returned a malformed change list."))

setattr(list_employee_changes_invalid_response_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_time_off_requests_listed(value: ListTimeOffRequestsOutput, failure: Failure | None = None) -> Case[ListTimeOffRequestsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_time_off_requests_listed, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_time_off_requests_provider_rejected(value: ListTimeOffRequestsOutput, failure: Failure | None = None) -> Case[ListTimeOffRequestsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_time_off_requests_provider_rejected, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_time_off_requests_invalid_response(value: ListTimeOffRequestsOutput, failure: Failure | None = None) -> Case[ListTimeOffRequestsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_time_off_requests_invalid_response, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_time_off_requests_defect(value: ListTimeOffRequestsOutput, failure: Failure | None = None) -> Case[ListTimeOffRequestsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_time_off_requests_defect, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_time_off_requests_retry(failure: Failure, after_seconds: float = 0) -> Case[ListTimeOffRequestsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_time_off_requests_retry, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_time_off_requests_listed_default_answer() -> Case[ListTimeOffRequestsOutput]:
    return Case.paged(LISTED, (ListTimeOffRequestsOutput(requests=(TimeOffRequest(id=502, employee_id='140', category_id=78, start_date='2026-10-26', end_date='2026-10-26', amount=1, unit='DAYS', status='DENIED', requested_at=datetime.fromisoformat("2026-09-03T15:04:05Z".replace('Z', '+00:00'))),), page=1, total_items=2, next_page=2), ListTimeOffRequestsOutput(requests=(TimeOffRequest(id=502, employee_id='140', category_id=78, start_date='2026-10-26', end_date='2026-10-26', amount=1, unit='DAYS', status='DENIED', requested_at=datetime.fromisoformat("2026-09-03T15:04:05Z".replace('Z', '+00:00'))),), page=2, total_items=3, next_page=3), ListTimeOffRequestsOutput(requests=(TimeOffRequest(id=501, employee_id='140', category_id=78, start_date='2026-10-20', end_date='2026-10-21', amount=2, unit='DAYS', status='APPROVED', requested_at=datetime.fromisoformat("2026-09-02T15:04:05Z".replace('Z', '+00:00'))),), page=3, total_items=3, next_page=0)))

setattr(list_time_off_requests_listed_default_answer, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_time_off_requests_provider_rejected_failure() -> Case[ListTimeOffRequestsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListTimeOffRequestsOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "listTimeOffRequests", "BambooHR rejected the time off filter."))

setattr(list_time_off_requests_provider_rejected_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def list_time_off_requests_invalid_response_failure() -> Case[ListTimeOffRequestsOutput]:
    return Case.branch_result(INVALID_RESPONSE, ListTimeOffRequestsOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listTimeOffRequests", "BambooHR returned a malformed time off page."))

setattr(list_time_off_requests_invalid_response_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def update_employee_updated(value: UpdateEmployeeOutput, failure: Failure | None = None) -> Case[UpdateEmployeeOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_employee_updated, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def update_employee_not_found(value: UpdateEmployeeOutput, failure: Failure | None = None) -> Case[UpdateEmployeeOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_employee_not_found, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def update_employee_provider_rejected(value: UpdateEmployeeOutput, failure: Failure | None = None) -> Case[UpdateEmployeeOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_employee_provider_rejected, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def update_employee_invalid_response(value: UpdateEmployeeOutput, failure: Failure | None = None) -> Case[UpdateEmployeeOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_employee_invalid_response, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def update_employee_defect(value: UpdateEmployeeOutput, failure: Failure | None = None) -> Case[UpdateEmployeeOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_employee_defect, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def update_employee_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateEmployeeOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_employee_retry, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def update_employee_updated_default_answer() -> Case[UpdateEmployeeOutput]:
    return Case.branch_result(UPDATED, UpdateEmployeeOutput(employee=EmployeeRecord(employee_id='140', fields={'department': 'Engineering'}), written_fields=('department',)))

setattr(update_employee_updated_default_answer, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def update_employee_not_found_failure() -> Case[UpdateEmployeeOutput]:
    return Case.branch_result(NOT_FOUND, UpdateEmployeeOutput(employee=EmployeeRecord()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateEmployee", "No employee has this internal ID."))

setattr(update_employee_not_found_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def update_employee_provider_rejected_failure() -> Case[UpdateEmployeeOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateEmployeeOutput(employee=EmployeeRecord()), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "updateEmployee", "BambooHR rejected the duplicate email."))

setattr(update_employee_provider_rejected_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def update_employee_invalid_response_failure() -> Case[UpdateEmployeeOutput]:
    return Case.branch_result(INVALID_RESPONSE, UpdateEmployeeOutput(employee=EmployeeRecord()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "updateEmployee", "BambooHR returned a malformed employee record, so the change may have been applied."))

setattr(update_employee_invalid_response_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def add_employee_created(value: AddEmployeeOutput, failure: Failure | None = None) -> Case[AddEmployeeOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(add_employee_created, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def add_employee_provider_rejected(value: AddEmployeeOutput, failure: Failure | None = None) -> Case[AddEmployeeOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_employee_provider_rejected, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def add_employee_uncertain(value: AddEmployeeOutput, failure: Failure) -> Case[AddEmployeeOutput]:
    return Case.uncertain(value, failure)

setattr(add_employee_uncertain, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def add_employee_defect(value: AddEmployeeOutput, failure: Failure | None = None) -> Case[AddEmployeeOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_employee_defect, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def add_employee_retry(failure: Failure, after_seconds: float = 0) -> Case[AddEmployeeOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_employee_retry, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def add_employee_created_default_answer() -> Case[AddEmployeeOutput]:
    return Case.branch_result(CREATED, AddEmployeeOutput(employee_id='142'))

setattr(add_employee_created_default_answer, '__module__', "dex_connectors.bamboohr.bamboohrmock")

def add_employee_provider_rejected_failure() -> Case[AddEmployeeOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddEmployeeOutput(), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "addEmployee", "BambooHR rejected the duplicate email."))

setattr(add_employee_provider_rejected_failure, '__module__', "dex_connectors.bamboohr.bamboohrmock")


class Mock:

    def __init__(self, connection_name: str = "bamboohr") -> None:
        self.get_employee_script: Query[GetEmployeeInput, EmployeeRecord] = Query(GET_EMPLOYEE_DEFINITION)
        self.get_employee_script.default(get_employee_found_default_answer())
        self.find_employee_by_email_script: Query[FindEmployeeByEmailInput, FindEmployeeByEmailOutput] = Query(FIND_EMPLOYEE_BY_EMAIL_DEFINITION)
        self.find_employee_by_email_script.default(find_employee_by_email_found_default_answer())
        self.list_employee_changes_script: Query[ListEmployeeChangesInput, ListEmployeeChangesOutput] = Query(LIST_EMPLOYEE_CHANGES_DEFINITION)
        self.list_employee_changes_script.default(list_employee_changes_listed_default_answer())
        self.list_time_off_requests_script: Query[ListTimeOffRequestsInput, ListTimeOffRequestsOutput] = Query(LIST_TIME_OFF_REQUESTS_DEFINITION)
        self.list_time_off_requests_script.default(list_time_off_requests_listed_default_answer())
        self.update_employee_script: Mutation[UpdateEmployeeInput, UpdateEmployeeOutput] = Mutation(UPDATE_EMPLOYEE_DEFINITION)
        self.update_employee_script.default(update_employee_updated_default_answer())
        self.add_employee_script: Mutation[AddEmployeeInput, AddEmployeeOutput] = Mutation(ADD_EMPLOYEE_DEFINITION)
        self.add_employee_script.default(add_employee_created_default_answer())
        self._connection = new_connection_with_operations(
            get_employee=self.get_employee_script,
            find_employee_by_email=self.find_employee_by_email_script,
            list_employee_changes=self.list_employee_changes_script,
            list_time_off_requests=self.list_time_off_requests_script,
            update_employee=self.update_employee_script,
            add_employee=self.add_employee_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.bamboohr.bamboohrmock")

__all__ = [
    "Mock",
    "get_employee_found",
    "get_employee_not_found",
    "get_employee_provider_rejected",
    "get_employee_invalid_response",
    "get_employee_defect",
    "get_employee_retry",
    "get_employee_found_default_answer",
    "get_employee_not_found_failure",
    "get_employee_provider_rejected_failure",
    "get_employee_invalid_response_failure",
    "find_employee_by_email_found",
    "find_employee_by_email_not_found",
    "find_employee_by_email_ambiguous",
    "find_employee_by_email_provider_rejected",
    "find_employee_by_email_invalid_response",
    "find_employee_by_email_defect",
    "find_employee_by_email_retry",
    "find_employee_by_email_found_default_answer",
    "find_employee_by_email_not_found_failure",
    "find_employee_by_email_ambiguous_failure",
    "find_employee_by_email_provider_rejected_failure",
    "find_employee_by_email_invalid_response_failure",
    "list_employee_changes_listed",
    "list_employee_changes_provider_rejected",
    "list_employee_changes_invalid_response",
    "list_employee_changes_defect",
    "list_employee_changes_retry",
    "list_employee_changes_listed_default_answer",
    "list_employee_changes_provider_rejected_failure",
    "list_employee_changes_invalid_response_failure",
    "list_time_off_requests_listed",
    "list_time_off_requests_provider_rejected",
    "list_time_off_requests_invalid_response",
    "list_time_off_requests_defect",
    "list_time_off_requests_retry",
    "list_time_off_requests_listed_default_answer",
    "list_time_off_requests_provider_rejected_failure",
    "list_time_off_requests_invalid_response_failure",
    "update_employee_updated",
    "update_employee_not_found",
    "update_employee_provider_rejected",
    "update_employee_invalid_response",
    "update_employee_defect",
    "update_employee_retry",
    "update_employee_updated_default_answer",
    "update_employee_not_found_failure",
    "update_employee_provider_rejected_failure",
    "update_employee_invalid_response_failure",
    "add_employee_created",
    "add_employee_provider_rejected",
    "add_employee_uncertain",
    "add_employee_defect",
    "add_employee_retry",
    "add_employee_created_default_answer",
    "add_employee_provider_rejected_failure",
]
