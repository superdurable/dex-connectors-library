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

CONNECTOR_ID = "chuanglan"
PROVIDER = "chuanglan"
GET_BALANCE_OPERATION_ID = "getBalance"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_BALANCE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_BALANCE_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "Chuanglan returned the remaining SMS count.", False),
        BranchDefinition(PROVIDER_REJECTED, "Chuanglan rejected the account, password, node, or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Chuanglan returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Connection credentials are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
