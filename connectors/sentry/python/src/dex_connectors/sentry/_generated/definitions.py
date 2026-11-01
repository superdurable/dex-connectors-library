# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation identities, branches, and Step defaults."""

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

CONNECTOR_ID = "sentry"
PROVIDER = "sentry"
LIST_ISSUES_OPERATION_ID = "listIssues"
GET_ISSUE_OPERATION_ID = "getIssue"
UPDATE_ISSUE_STATUS_OPERATION_ID = "updateIssueStatus"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UPDATED = "updated"

LIST_ISSUES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ISSUES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Sentry returned the requested result.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
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
        BranchDefinition(FOUND, "Sentry returned the requested result.", False),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_ISSUE_STATUS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_ISSUE_STATUS_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Sentry returned the requested result.", False),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
