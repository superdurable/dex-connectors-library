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

CONNECTOR_ID = "tianyancha"
PROVIDER = "tianyancha"
GET_COMPANY_PROFILE_OPERATION_ID = "getCompanyProfile"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NO_DATA = "noData"
PROVIDER_REJECTED = "providerRejected"

GET_COMPANY_PROFILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_COMPANY_PROFILE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Tianyancha returned one company profile.", False),
        BranchDefinition(NO_DATA, "Tianyancha found no matching company.", True),
        BranchDefinition(PROVIDER_REJECTED, "Tianyancha rejected the token, permission, or query.", True),
        BranchDefinition(INVALID_RESPONSE, "Tianyancha returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local token or company keyword is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
