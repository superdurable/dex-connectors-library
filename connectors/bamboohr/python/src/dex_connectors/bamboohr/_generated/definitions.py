# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "bamboohr"
PROVIDER = "bamboohr"
GET_EMPLOYEE_OPERATION_ID = "getEmployee"
FIND_EMPLOYEE_BY_EMAIL_OPERATION_ID = "findEmployeeByEmail"
LIST_EMPLOYEE_CHANGES_OPERATION_ID = "listEmployeeChanges"
LIST_TIME_OFF_REQUESTS_OPERATION_ID = "listTimeOffRequests"
UPDATE_EMPLOYEE_OPERATION_ID = "updateEmployee"
ADD_EMPLOYEE_OPERATION_ID = "addEmployee"
AMBIGUOUS = "ambiguous"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"
UPDATED = "updated"

GET_EMPLOYEE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_EMPLOYEE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The employee was read; fields the connection cannot see are listed as omitted.", False),
        BranchDefinition(NOT_FOUND, "No employee has this internal ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "BambooHR conclusively rejected the read, such as invalid credentials or a user who cannot view this employee.", True),
        BranchDefinition(INVALID_RESPONSE, "BambooHR returned an invalid or oversized employee record.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

FIND_EMPLOYEE_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_EMPLOYEE_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Exactly one employee visible to the connection has this email.", False),
        BranchDefinition(NOT_FOUND, "No employee visible to the connection has this email; BambooHR drops employees whose email the connection cannot read.", True),
        BranchDefinition(AMBIGUOUS, "More than one employee has this email, or more than 100 employee emails contain it, so the connector cannot pick one.", True),
        BranchDefinition(PROVIDER_REJECTED, "BambooHR conclusively rejected the search, such as invalid credentials or an unsupported field.", True),
        BranchDefinition(INVALID_RESPONSE, "BambooHR returned an invalid or oversized employee list.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_EMPLOYEE_CHANGES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_EMPLOYEE_CHANGES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "The changes after the cursor were listed; the list may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "BambooHR conclusively rejected the request, such as invalid credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "BambooHR returned an invalid or oversized change list.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_TIME_OFF_REQUESTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TIME_OFF_REQUESTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of overlapping time off requests was returned; it may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "BambooHR conclusively rejected the request, such as invalid credentials or an invalid filter.", True),
        BranchDefinition(INVALID_RESPONSE, "BambooHR returned an invalid or oversized time off page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_EMPLOYEE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_EMPLOYEE_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The employee holds the requested values, written now or by an earlier attempt, as read back from BambooHR.", False),
        BranchDefinition(NOT_FOUND, "No employee has this internal ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "BambooHR conclusively rejected the change, such as a duplicate email, an invalid list value, or a field the connection cannot edit.", True),
        BranchDefinition(INVALID_RESPONSE, "BambooHR returned an invalid or oversized employee record, so the change may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_EMPLOYEE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_EMPLOYEE_OPERATION_ID),
    (
        BranchDefinition(CREATED, "BambooHR added the employee and returned its internal ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "BambooHR conclusively rejected the employee, such as a duplicate email or invalid field value; no employee was added.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but its outcome cannot be confirmed, so the employee may exist; the connector never resends it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)
