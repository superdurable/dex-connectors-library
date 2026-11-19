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

CONNECTOR_ID = "enlvon"
PROVIDER = "enlvon"
SEARCH_POLICIES_OPERATION_ID = "searchPolicies"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

SEARCH_POLICIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_POLICIES_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Enlvon returned policy cards.", False),
        BranchDefinition(PROVIDER_REJECTED, "Enlvon rejected credentials or input.", True),
        BranchDefinition(INVALID_RESPONSE, "Enlvon returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or query are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 3, timedelta(seconds=180)),
    ),
)
