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

CONNECTOR_ID = "lever"
PROVIDER = "lever"
GET_OPPORTUNITY_OPERATION_ID = "getOpportunity"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_OPPORTUNITY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_OPPORTUNITY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Lever returned the opportunity.", False),
        BranchDefinition(NOT_FOUND, "Lever did not expose the opportunity.", True),
        BranchDefinition(PROVIDER_REJECTED, "Lever conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Lever returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
