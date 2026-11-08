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

CONNECTOR_ID = "google-search-console"
PROVIDER = "google-search-console"
LIST_SITES_OPERATION_ID = "listSites"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_SITES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SITES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Google returned the accessible properties.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
