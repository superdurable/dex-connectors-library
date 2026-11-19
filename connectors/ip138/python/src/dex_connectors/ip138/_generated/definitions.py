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

CONNECTOR_ID = "ip138"
PROVIDER = "ip138"
GET_TOKEN_USAGE_OPERATION_ID = "getTokenUsage"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_TOKEN_USAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TOKEN_USAGE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "iP138 returned token usage and quota.", False),
        BranchDefinition(PROVIDER_REJECTED, "iP138 rejected the token or request.", True),
        BranchDefinition(INVALID_RESPONSE, "iP138 returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local token is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
