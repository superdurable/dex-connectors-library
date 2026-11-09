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

CONNECTOR_ID = "amplitude"
PROVIDER = "amplitude"
SEARCH_USERS_OPERATION_ID = "searchUsers"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"

SEARCH_USERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_USERS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "Amplitude returned matching user IDs, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Amplitude conclusively rejected the search.", True),
        BranchDefinition(INVALID_RESPONSE, "Amplitude returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
