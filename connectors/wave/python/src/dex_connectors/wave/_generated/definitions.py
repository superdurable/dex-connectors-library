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

CONNECTOR_ID = "wave"
PROVIDER = "wave"
GET_CUSTOMER_OPERATION_ID = "getCustomer"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_CUSTOMER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CUSTOMER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Wave returned the customer.", False),
        BranchDefinition(NOT_FOUND, "The business or customer was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "Wave conclusively rejected the query.", True),
        BranchDefinition(INVALID_RESPONSE, "Wave returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
