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

CONNECTOR_ID = "bocha"
PROVIDER = "bocha"
SEARCH_WEB_OPERATION_ID = "searchWeb"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

SEARCH_WEB_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_WEB_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Bocha returned public web search results.", False),
        BranchDefinition(PROVIDER_REJECTED, "Bocha rejected the Key or query.", True),
        BranchDefinition(INVALID_RESPONSE, "Bocha returned malformed or oversized search data.", True),
        BranchDefinition(DEFECT, "The local query or API Key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=10), 2, timedelta(seconds=60)),
    ),
)
