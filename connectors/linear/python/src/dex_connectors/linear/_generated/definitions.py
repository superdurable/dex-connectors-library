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

CONNECTOR_ID = "linear"
PROVIDER = "linear"
SEARCH_ISSUES_OPERATION_ID = "searchIssues"
GET_ISSUE_OPERATION_ID = "getIssue"
CREATE_ISSUE_OPERATION_ID = "createIssue"
UPDATE_ISSUE_OPERATION_ID = "updateIssue"
ADD_COMMENT_OPERATION_ID = "addComment"
FIND_USER_BY_EMAIL_OPERATION_ID = "findUserByEmail"
LIST_WORKFLOW_STATES_OPERATION_ID = "listWorkflowStates"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"
UPDATED = "updated"

SEARCH_ISSUES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_ISSUES_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of matching issue summaries was read; it can be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Linear conclusively rejected the search, such as an invalid cursor, missing team access, or a rejected credential.", True),
        BranchDefinition(INVALID_RESPONSE, "Linear returned an invalid or oversized page of issues.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_ISSUE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ISSUE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The issue was read.", False),
        BranchDefinition(NOT_FOUND, "The issue does not exist, was deleted, or the connection cannot see its team.", True),
        BranchDefinition(PROVIDER_REJECTED, "Linear conclusively rejected the issue read.", True),
        BranchDefinition(INVALID_RESPONSE, "Linear returned an invalid or oversized issue.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_ISSUE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_ISSUE_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Linear created the issue, or an earlier attempt of this Step already had, and the issue was read.", False),
        BranchDefinition(PROVIDER_REJECTED, "Linear conclusively rejected the issue, such as an unknown team, a workflow state or label of another team, or missing permission; unless Linear rejected the credential or permission, a read-back by the Step's UUID found no issue.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_ISSUE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_ISSUE_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Linear applied the change and returned the issue.", False),
        BranchDefinition(NOT_FOUND, "The issue does not exist, was deleted, or the connection cannot see its team; nothing was changed.", True),
        BranchDefinition(PROVIDER_REJECTED, "Linear conclusively rejected the change, such as a workflow state or label of another team, an unknown assignee, or missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Linear returned an invalid or oversized issue, so the change may have been applied; repeating the Step is safe.", True),
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
        BranchDefinition(ADDED, "Linear added the comment, or an earlier attempt of this Step already had, and the comment was read.", False),
        BranchDefinition(NOT_FOUND, "The issue does not exist, was deleted, or the connection cannot see its team; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Linear conclusively rejected the comment, such as missing permission; unless Linear rejected the credential or permission, a read-back by the Step's UUID found no comment.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

FIND_USER_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_USER_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The user was found.", False),
        BranchDefinition(NOT_FOUND, "No user of the workspace has the email, or the user is disabled and disabled users were not requested.", True),
        BranchDefinition(PROVIDER_REJECTED, "Linear conclusively rejected the user lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Linear returned an invalid or oversized user list.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_WORKFLOW_STATES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_WORKFLOW_STATES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "The team's workflow states were read.", False),
        BranchDefinition(NOT_FOUND, "The team does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Linear conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Linear returned an invalid or oversized list of workflow states.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
