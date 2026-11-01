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

CONNECTOR_ID = "bitbucket"
PROVIDER = "bitbucket"
LIST_REPOSITORIES_OPERATION_ID = "listRepositories"
LIST_PULL_REQUESTS_OPERATION_ID = "listPullRequests"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_REPOSITORIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_REPOSITORIES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List repositories owned by a Bitbucket workspace.", False),
        BranchDefinition(NOT_FOUND, "The requested resource was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_PULL_REQUESTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PULL_REQUESTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List pull requests for a Bitbucket repository.", False),
        BranchDefinition(NOT_FOUND, "The requested resource was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
