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

CONNECTOR_ID = "yuandian"
PROVIDER = "yuandian"
GET_COMPANY_PROFILE_OPERATION_ID = "getCompanyProfile"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NO_DATA = "noData"
PROVIDER_REJECTED = "providerRejected"

GET_COMPANY_PROFILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_COMPANY_PROFILE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Yuandian returned one company profile.", False),
        BranchDefinition(NO_DATA, "Yuandian found no company for the credit code.", True),
        BranchDefinition(PROVIDER_REJECTED, "Yuandian rejected the key, permission, or query.", True),
        BranchDefinition(INVALID_RESPONSE, "Yuandian returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local key or credit code is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
