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

CONNECTOR_ID = "customerio"
PROVIDER = "customerio"
GET_CUSTOMERS_BY_EMAIL_OPERATION_ID = "getCustomersByEmail"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_CUSTOMERS_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CUSTOMERS_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Customer.io returned matching customer IDs, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Customer.io conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Customer.io returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
