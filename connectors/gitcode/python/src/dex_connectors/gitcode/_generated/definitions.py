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

CONNECTOR_ID = "gitcode"
PROVIDER = "gitcode"
GET_REPOSITORY_OPERATION_ID = "getRepository"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_REPOSITORY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_REPOSITORY_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "GitCode returned the repository.", False),
        BranchDefinition(NOT_FOUND, "The repository does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "GitCode rejected the token or request.", True),
        BranchDefinition(INVALID_RESPONSE, "GitCode returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local owner/repository path or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
