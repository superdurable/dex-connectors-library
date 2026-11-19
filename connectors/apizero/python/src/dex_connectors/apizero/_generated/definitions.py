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

CONNECTOR_ID = "apizero"
PROVIDER = "apizero"
GET_ACCOUNT_INFO_OPERATION_ID = "getAccountInfo"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_ACCOUNT_INFO_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ACCOUNT_INFO_OPERATION_ID),
    (
        BranchDefinition(FOUND, "ApiZero returned account or prepaid Key information.", False),
        BranchDefinition(PROVIDER_REJECTED, "ApiZero rejected the Key or request.", True),
        BranchDefinition(INVALID_RESPONSE, "ApiZero returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local API Key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
