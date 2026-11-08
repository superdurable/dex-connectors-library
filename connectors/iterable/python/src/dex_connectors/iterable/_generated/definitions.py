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

CONNECTOR_ID = "iterable"
PROVIDER = "iterable"
GET_USER_OPERATION_ID = "getUser"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_USER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_USER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Iterable returned the user.", False),
        BranchDefinition(NOT_FOUND, "Iterable did not expose the user.", True),
        BranchDefinition(PROVIDER_REJECTED, "Iterable conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Iterable returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
