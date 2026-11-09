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

CONNECTOR_ID = "oracle-netsuite"
PROVIDER = "oracle-netsuite"
GET_CUSTOMER_OPERATION_ID = "getCustomer"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_CUSTOMER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CUSTOMER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "NetSuite returned the customer.", False),
        BranchDefinition(NOT_FOUND, "No accessible customer has this internal ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Oracle NetSuite conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "NetSuite returned malformed or oversized customer JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
