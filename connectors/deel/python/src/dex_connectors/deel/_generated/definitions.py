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

CONNECTOR_ID = "deel"
PROVIDER = "deel"
GET_CONTRACT_OPERATION_ID = "getContract"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_CONTRACT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CONTRACT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Deel returned the contract.", False),
        BranchDefinition(NOT_FOUND, "Deel did not expose the contract.", True),
        BranchDefinition(PROVIDER_REJECTED, "Deel conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Deel returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
