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

CONNECTOR_ID = "sendcloud"
PROVIDER = "sendcloud"
GET_ACCOUNT_BALANCE_OPERATION_ID = "getAccountBalance"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
RETRIEVED = "retrieved"

GET_ACCOUNT_BALANCE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ACCOUNT_BALANCE_OPERATION_ID),
    (
        BranchDefinition(RETRIEVED, "SendCloud returned account balance and daily quota counters.", False),
        BranchDefinition(PROVIDER_REJECTED, "SendCloud rejected credentials or access to the user information API.", True),
        BranchDefinition(INVALID_RESPONSE, "SendCloud returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
