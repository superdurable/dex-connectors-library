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

CONNECTOR_ID = "minimax"
PROVIDER = "minimax"
GET_TOKEN_PLAN_REMAINS_OPERATION_ID = "getTokenPlanRemains"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_TOKEN_PLAN_REMAINS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TOKEN_PLAN_REMAINS_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "MiniMax returned Token Plan usage for zero or more model groups.", False),
        BranchDefinition(PROVIDER_REJECTED, "MiniMax rejected the key or plan state.", True),
        BranchDefinition(INVALID_RESPONSE, "MiniMax returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Connection configuration or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
