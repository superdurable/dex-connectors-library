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

CONNECTOR_ID = "zentao"
PROVIDER = "zentao"
LIST_PRODUCTS_OPERATION_ID = "listProducts"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_PRODUCTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PRODUCTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "ZenTao returned a product list, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "ZenTao rejected the token or permissions.", True),
        BranchDefinition(INVALID_RESPONSE, "ZenTao returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection configuration or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
