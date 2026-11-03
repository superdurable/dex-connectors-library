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

CONNECTOR_ID = "asana"
PROVIDER = "asana"
LIST_TASKS_OPERATION_ID = "listTasks"
GET_TASK_OPERATION_ID = "getTask"
CREATE_TASK_OPERATION_ID = "createTask"
UPDATE_TASK_OPERATION_ID = "updateTask"
ADD_COMMENT_OPERATION_ID = "addComment"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"
UPDATED = "updated"

LIST_TASKS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TASKS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of the project's or section's tasks was read.", False),
        BranchDefinition(NOT_FOUND, "The project or section does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Asana conclusively rejected the list, such as an expired offset token, missing permission, or a result set too large to page.", True),
        BranchDefinition(INVALID_RESPONSE, "Asana returned an invalid or oversized task page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_TASK_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TASK_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The task was read.", False),
        BranchDefinition(NOT_FOUND, "The task does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Asana conclusively rejected the task read.", True),
        BranchDefinition(INVALID_RESPONSE, "Asana returned an invalid or oversized task.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
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
        BranchDefinition(CREATED, "Asana created the task and returned its ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "Asana conclusively rejected the task, such as an unknown project, section, assignee, or custom field value, or missing permission; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "The request may have reached Asana but its outcome cannot be confirmed; Asana has no idempotency key, so the task may exist and must not be created again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

UPDATE_TASK_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_TASK_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Asana applied every requested change, including when the task already had those values, and the Value holds the task read back.", False),
        BranchDefinition(NOT_FOUND, "The task or section does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Asana conclusively rejected a change, such as an unknown assignee, a section of a project the task is not in, or missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Asana returned an invalid or oversized task after the update.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_COMMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_COMMENT_OPERATION_ID),
    (
        BranchDefinition(ADDED, "Asana added the comment and returned its story ID.", False),
        BranchDefinition(NOT_FOUND, "The task does not exist or the connection cannot see it; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Asana conclusively rejected the comment, such as missing permission or invalid text; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "The request may have reached Asana but its outcome cannot be confirmed; Asana has no idempotency key, so the comment may exist and must not be added again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
