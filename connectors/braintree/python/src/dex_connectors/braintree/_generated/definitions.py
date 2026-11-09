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

CONNECTOR_ID = "braintree"
PROVIDER = "braintree"
GET_TRANSACTION_OPERATION_ID = "getTransaction"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_TRANSACTION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TRANSACTION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Braintree returned the transaction.", False),
        BranchDefinition(NOT_FOUND, "The node is absent or is another object type.", True),
        BranchDefinition(PROVIDER_REJECTED, "Braintree returned a definite GraphQL or HTTP rejection.", True),
        BranchDefinition(INVALID_RESPONSE, "Braintree returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
