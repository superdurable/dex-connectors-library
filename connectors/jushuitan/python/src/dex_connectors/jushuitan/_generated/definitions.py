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

CONNECTOR_ID = "jushuitan"
PROVIDER = "jushuitan"
LIST_SHOPS_OPERATION_ID = "listShops"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_SHOPS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SHOPS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Jushuitan returned one page of enabled shops.", False),
        BranchDefinition(PROVIDER_REJECTED, "Jushuitan rejected the shop-list request.", True),
        BranchDefinition(INVALID_RESPONSE, "Jushuitan returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection credentials or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
