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

CONNECTOR_ID = "chukou1"
PROVIDER = "chukou1"
LIST_WAREHOUSES_OPERATION_ID = "listWarehouses"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_WAREHOUSES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_WAREHOUSES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Chukou1 returned overseas warehouses, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Chukou1 rejected the token or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Chukou1 returned malformed or oversized warehouse data.", True),
        BranchDefinition(DEFECT, "Local configuration or token is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
