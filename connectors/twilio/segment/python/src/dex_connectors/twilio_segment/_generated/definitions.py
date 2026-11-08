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

CONNECTOR_ID = "segment"
PROVIDER = "segment"
GET_SOURCE_OPERATION_ID = "getSource"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_SOURCE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SOURCE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Segment returned the Source.", False),
        BranchDefinition(NOT_FOUND, "Segment did not expose the Source.", True),
        BranchDefinition(PROVIDER_REJECTED, "Segment conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Segment returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
