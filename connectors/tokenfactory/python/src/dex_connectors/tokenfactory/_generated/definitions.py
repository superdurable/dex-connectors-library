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

CONNECTOR_ID = "tokenfactory"
PROVIDER = "tokenfactory"
GET_BALANCE_OPERATION_ID = "getBalance"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
RETRIEVED = "retrieved"

GET_BALANCE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_BALANCE_OPERATION_ID),
    (
        BranchDefinition(RETRIEVED, "TokenFactory returned the account balance and Key status.", False),
        BranchDefinition(PROVIDER_REJECTED, "TokenFactory rejected the API Key or balance query.", True),
        BranchDefinition(INVALID_RESPONSE, "TokenFactory returned malformed or oversized balance data.", True),
        BranchDefinition(DEFECT, "Local connection configuration or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
