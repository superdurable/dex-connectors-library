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

CONNECTOR_ID = "todoist"
PROVIDER = "todoist"
GET_TASK_OPERATION_ID = "getTask"
UPDATE_TASK_CONTENT_OPERATION_ID = "updateTaskContent"
LIST_TASKS_OPERATION_ID = "listTasks"
CREATE_TASK_OPERATION_ID = "createTask"
DELETE_TASK_OPERATION_ID = "deleteTask"
ALREADY_ABSENT = "alreadyAbsent"
CREATED = "created"
DEFECT = "defect"
DELETED = "deleted"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"
UPDATED = "updated"

GET_TASK_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TASK_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Todoist returned the active task.", False),
        BranchDefinition(NOT_FOUND, "The active task does not exist or is not accessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Todoist conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Todoist returned malformed or oversized task data.", True),
        BranchDefinition(DEFECT, "Local input is invalid, or the connection credentials are invalid, missing, or need reauthorization.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_TASK_CONTENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_TASK_CONTENT_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Todoist confirmed every specified field and returned the task.", False),
        BranchDefinition(NOT_FOUND, "Todoist reports that the task does not exist, or the read-back after an unusable answer found no active task.", True),
        BranchDefinition(PROVIDER_REJECTED, "Todoist conclusively rejected the update, or rejected the read-back that would confirm an unusable answer.", True),
        BranchDefinition(DEFECT, "Local input is invalid, or the connection credentials are invalid, missing, or need reauthorization.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_TASKS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TASKS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Todoist returned a page of active tasks, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Todoist conclusively rejected the list request.", True),
        BranchDefinition(INVALID_RESPONSE, "Todoist returned malformed or oversized page data.", True),
        BranchDefinition(DEFECT, "Local input is invalid, or the connection credentials are invalid, missing, or need reauthorization.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_TASK_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_TASK_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Todoist confirmed creation and returned the task ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "Todoist conclusively rejected creation.", True),
        BranchDefinition(UNCERTAIN, "A creation that may have been sent may have applied, but Todoist returned no usable task and the request ID has no documented replay guarantee.", True),
        BranchDefinition(DEFECT, "Local input is invalid, or the connection credentials are invalid, missing, or need reauthorization.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

DELETE_TASK_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, DELETE_TASK_OPERATION_ID),
    (
        BranchDefinition(DELETED, "Todoist acknowledged the deletion.", False),
        BranchDefinition(ALREADY_ABSENT, "Todoist returned 404; the task is missing or inaccessible, possibly after an earlier successful attempt.", True),
        BranchDefinition(PROVIDER_REJECTED, "Todoist conclusively rejected deletion.", True),
        BranchDefinition(DEFECT, "Local input is invalid, or the connection credentials are invalid, missing, or need reauthorization.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
