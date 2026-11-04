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

CONNECTOR_ID = "gitlab"
PROVIDER = "gitlab"
GET_PROJECT_OPERATION_ID = "getProject"
LIST_ISSUES_OPERATION_ID = "listIssues"
UPDATE_ISSUE_LABELS_OPERATION_ID = "updateIssueLabels"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UPDATED = "updated"

GET_PROJECT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PROJECT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Read one GitLab project by numeric ID or namespace path.", False),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_ISSUES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ISSUES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List the first page of issues for a GitLab project.", False),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_ISSUE_LABELS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_ISSUE_LABELS_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Replace one GitLab issue's complete label set; repeating the request is safe.", False),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
