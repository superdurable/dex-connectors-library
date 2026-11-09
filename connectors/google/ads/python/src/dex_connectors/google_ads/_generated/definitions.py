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

CONNECTOR_ID = "google-ads"
PROVIDER = "google-ads"
LIST_ACCESSIBLE_CUSTOMERS_OPERATION_ID = "listAccessibleCustomers"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_ACCESSIBLE_CUSTOMERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ACCESSIBLE_CUSTOMERS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Google Ads returned the directly accessible customer names.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google Ads conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Google Ads returned malformed or oversized customer JSON.", True),
        BranchDefinition(DEFECT, "Local result limit or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
