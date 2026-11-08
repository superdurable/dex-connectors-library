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

CONNECTOR_ID = "wrike"
PROVIDER = "wrike"
GET_TASK_OPERATION_ID = "getTask"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_TASK_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TASK_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Wrike returned the task.", False),
        BranchDefinition(NOT_FOUND, "Wrike did not expose the task.", True),
        BranchDefinition(PROVIDER_REJECTED, "Wrike conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Wrike returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
