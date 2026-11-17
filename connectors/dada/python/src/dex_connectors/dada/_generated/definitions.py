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

CONNECTOR_ID = "dada"
PROVIDER = "dada"
QUERY_ORDER_STATUS_OPERATION_ID = "queryOrderStatus"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

QUERY_ORDER_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_ORDER_STATUS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Dada returned the order's current delivery status.", False),
        BranchDefinition(PROVIDER_REJECTED, "Dada conclusively rejected the status query.", True),
        BranchDefinition(INVALID_RESPONSE, "Dada returned malformed or oversized status JSON.", True),
        BranchDefinition(DEFECT, "The local credentials or order ID are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
