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

CONNECTOR_ID = "paddle"
PROVIDER = "paddle"
GET_TRANSACTION_OPERATION_ID = "getTransaction"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_TRANSACTION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TRANSACTION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Paddle returned the transaction.", False),
        BranchDefinition(NOT_FOUND, "Paddle did not expose the transaction.", True),
        BranchDefinition(PROVIDER_REJECTED, "Paddle conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Paddle returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
