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

CONNECTOR_ID = "clickup"
PROVIDER = "clickup"
SEARCH_TASKS_OPERATION_ID = "searchTasks"
GET_TASK_OPERATION_ID = "getTask"
CREATE_TASK_OPERATION_ID = "createTask"
UPDATE_TASK_OPERATION_ID = "updateTask"
UPDATE_TASK_TAGS_OPERATION_ID = "updateTaskTags"
ADD_COMMENT_OPERATION_ID = "addComment"
FIND_MEMBER_BY_EMAIL_OPERATION_ID = "findMemberByEmail"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"
UNCERTAIN = "uncertain"
UPDATED = "updated"

SEARCH_TASKS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_TASKS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of matching tasks was read; it can be empty.", False),
        BranchDefinition(NOT_FOUND, "The Workspace does not exist or the token's user is not a member of it.", True),
        BranchDefinition(PROVIDER_REJECTED, "ClickUp conclusively rejected the search, such as an invalid filter or an invalid token.", True),
        BranchDefinition(INVALID_RESPONSE, "ClickUp returned an invalid or oversized page of tasks.", True),
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
        BranchDefinition(PROVIDER_REJECTED, "ClickUp conclusively rejected the task read.", True),
        BranchDefinition(INVALID_RESPONSE, "ClickUp returned an invalid or oversized task.", True),
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
        BranchDefinition(CREATED, "ClickUp created the task; a retried attempt that found its own earlier task in the List reports it instead of creating another.", False),
        BranchDefinition(NOT_FOUND, "The List does not exist or the connection cannot see it; nothing was created.", True),
        BranchDefinition(PROVIDER_REJECTED, "ClickUp conclusively rejected the task, such as an unknown status, assignee, or custom field value, or missing permission; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "An earlier attempt sent the create but its outcome cannot be confirmed and the List shows no single matching task, so the task may exist; the connector never sends it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)

UPDATE_TASK_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_TASK_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "ClickUp applied every requested change, including when the task already had those values, and the Value holds the task it returned.", False),
        BranchDefinition(NOT_FOUND, "The task does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "ClickUp conclusively rejected the change, such as a status that is not in the task's List or missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "ClickUp returned an invalid or oversized task after the update, so the change may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_TASK_TAGS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_TASK_TAGS_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "ClickUp applied every tag change, and the Value holds the task's tags read back.", False),
        BranchDefinition(NOT_FOUND, "The task does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "ClickUp conclusively rejected a tag change, such as missing permission; earlier changes in the same Step may have been applied.", True),
        BranchDefinition(INVALID_RESPONSE, "ClickUp returned an invalid or oversized task when its tags were read back; the tag changes were applied.", True),
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
        BranchDefinition(ADDED, "The comment is on the task; a retried attempt that found its own earlier comment reports it instead of adding another.", False),
        BranchDefinition(NOT_FOUND, "The task does not exist or the connection cannot see it; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "ClickUp conclusively rejected the comment, such as missing permission; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "An earlier attempt sent the comment but its outcome cannot be confirmed and the task shows no matching comment, so the comment may exist; the connector never sends it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)

FIND_MEMBER_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_MEMBER_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "A member of the Workspace has the email address.", False),
        BranchDefinition(NOT_FOUND, "No member of the Workspace has the email address, or the token's user is not a member of the Workspace.", True),
        BranchDefinition(PROVIDER_REJECTED, "ClickUp conclusively rejected the Workspace read.", True),
        BranchDefinition(INVALID_RESPONSE, "ClickUp returned an invalid or oversized list of Workspaces.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
