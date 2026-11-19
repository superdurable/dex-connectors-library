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

CONNECTOR_ID = "meituan"
PROVIDER = "meituan"
QUERY_ORDER_STATUS_OPERATION_ID = "queryOrderStatus"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

QUERY_ORDER_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_ORDER_STATUS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Meituan returned the current order status.", False),
        BranchDefinition(PROVIDER_REJECTED, "Meituan rejected the credentials or order identifiers.", True),
        BranchDefinition(INVALID_RESPONSE, "Meituan returned malformed or oversized order data.", True),
        BranchDefinition(DEFECT, "The local order identifiers or connection are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
