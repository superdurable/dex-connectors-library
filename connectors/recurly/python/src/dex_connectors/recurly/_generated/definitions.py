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

CONNECTOR_ID = "recurly"
PROVIDER = "recurly"
GET_ACCOUNT_OPERATION_ID = "getAccount"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_ACCOUNT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ACCOUNT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Recurly returned the account.", False),
        BranchDefinition(NOT_FOUND, "Recurly did not expose the account.", True),
        BranchDefinition(PROVIDER_REJECTED, "Recurly conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Recurly returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
