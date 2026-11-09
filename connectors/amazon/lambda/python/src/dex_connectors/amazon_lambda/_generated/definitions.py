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

CONNECTOR_ID = "amazon-lambda"
PROVIDER = "amazon-lambda"
GET_FUNCTION_OPERATION_ID = "getFunction"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MISSING = "missing"
PROVIDER_REJECTED = "providerRejected"

GET_FUNCTION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_FUNCTION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "AWS returned the function configuration.", False),
        BranchDefinition(MISSING, "The function does not exist in the selected Region.", True),
        BranchDefinition(PROVIDER_REJECTED, "AWS conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "AWS returned malformed or oversized function metadata.", True),
        BranchDefinition(DEFECT, "The function name, endpoint, Region, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
