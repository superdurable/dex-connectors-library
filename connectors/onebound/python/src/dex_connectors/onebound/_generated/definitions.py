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

CONNECTOR_ID = "onebound"
PROVIDER = "onebound"
SEARCH_PRODUCTS_OPERATION_ID = "searchProducts"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

SEARCH_PRODUCTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_PRODUCTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Onebound returned a product page, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Onebound rejected the credentials, plan, quota, or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Onebound returned malformed or oversized product data.", True),
        BranchDefinition(DEFECT, "Local keyword, page, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=10), 3, timedelta(seconds=60)),
    ),
)
