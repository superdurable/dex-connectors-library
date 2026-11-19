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

CONNECTOR_ID = "cloopen"
PROVIDER = "cloopen"
GET_BALANCE_OPERATION_ID = "getBalance"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_BALANCE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_BALANCE_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "Cloopen returned the main-account balance.", False),
        BranchDefinition(PROVIDER_REJECTED, "Cloopen rejected the account or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Cloopen returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Connection credentials are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
