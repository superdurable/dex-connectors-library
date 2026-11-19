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

CONNECTOR_ID = "smzdm"
PROVIDER = "smzdm"
SEARCH_PRODUCTS_OPERATION_ID = "searchProducts"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

SEARCH_PRODUCTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_PRODUCTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "SMZDM returned a first page of products, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "SMZDM rejected the credentials, authorization, or query.", True),
        BranchDefinition(INVALID_RESPONSE, "SMZDM returned malformed or oversized product data.", True),
        BranchDefinition(DEFECT, "Local configuration, credentials, or keyword are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
