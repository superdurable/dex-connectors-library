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

CONNECTOR_ID = "gitee"
PROVIDER = "gitee"
GET_REPOSITORY_OPERATION_ID = "getRepository"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_REPOSITORY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_REPOSITORY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Gitee returned repository metadata.", False),
        BranchDefinition(NOT_FOUND, "The repository was not found or is not visible to this token.", True),
        BranchDefinition(PROVIDER_REJECTED, "Gitee rejected the token or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Gitee returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
