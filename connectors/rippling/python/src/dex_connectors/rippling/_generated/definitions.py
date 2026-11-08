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

CONNECTOR_ID = "rippling"
PROVIDER = "rippling"
GET_WORKER_OPERATION_ID = "getWorker"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_WORKER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_WORKER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Rippling returned the worker.", False),
        BranchDefinition(NOT_FOUND, "Rippling did not expose the worker.", True),
        BranchDefinition(PROVIDER_REJECTED, "Rippling conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Rippling returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
