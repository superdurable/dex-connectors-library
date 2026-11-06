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

CONNECTOR_ID = "jira"
PROVIDER = "atlassian"
SEARCH_ISSUES_OPERATION_ID = "searchIssues"
GET_ISSUE_OPERATION_ID = "getIssue"
CREATE_ISSUE_OPERATION_ID = "createIssue"
TRANSITION_ISSUE_OPERATION_ID = "transitionIssue"
ADD_COMMENT_OPERATION_ID = "addComment"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"
TRANSITION_UNAVAILABLE = "transitionUnavailable"
TRANSITIONED = "transitioned"
UNCERTAIN = "uncertain"

SEARCH_ISSUES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_ISSUES_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of matching issues was read; Jira search is eventually consistent, so a just-created issue can be missing.", False),
        BranchDefinition(PROVIDER_REJECTED, "Jira conclusively rejected the search, such as invalid or unbounded JQL, an expired page token, or missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Jira returned an invalid or oversized search page.", True),
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
        BranchDefinition(NOT_FOUND, "The issue does not exist or the connection cannot browse it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Jira conclusively rejected the issue read.", True),
        BranchDefinition(INVALID_RESPONSE, "Jira returned an invalid or oversized issue.", True),
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
        BranchDefinition(CREATED, "Jira created the issue and returned its key and ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "Jira conclusively rejected the issue, such as a missing required field, an unknown project or issue type, or missing permission; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "The request may have reached Jira but could not be confirmed; the issue may exist and must not be created again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

TRANSITION_ISSUE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, TRANSITION_ISSUE_OPERATION_ID),
    (
        BranchDefinition(TRANSITIONED, "The issue is in the transition's destination status, including when it already was.", False),
        BranchDefinition(NOT_FOUND, "The issue does not exist or the connection cannot browse it.", True),
        BranchDefinition(TRANSITION_UNAVAILABLE, "No available transition from the issue's current status matches the request, or several do; the Value lists the current status and the available transitions.", True),
        BranchDefinition(PROVIDER_REJECTED, "Jira conclusively rejected the transition, such as a required transition screen field or missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Jira returned an invalid or oversized issue or transition list.", True),
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
        BranchDefinition(ADDED, "Jira added the comment and returned its ID.", False),
        BranchDefinition(NOT_FOUND, "The issue does not exist or the connection cannot browse it; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Jira conclusively rejected the comment, such as missing Add comments permission or the per-issue comment limit; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "The request may have reached Jira but could not be confirmed; the comment may exist and must not be added again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
