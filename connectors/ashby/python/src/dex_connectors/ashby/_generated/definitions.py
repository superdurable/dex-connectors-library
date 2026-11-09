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

CONNECTOR_ID = "ashby"
PROVIDER = "ashby"
GET_CANDIDATE_OPERATION_ID = "getCandidate"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_CANDIDATE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CANDIDATE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Ashby returned the candidate.", False),
        BranchDefinition(NOT_FOUND, "Ashby did not expose the candidate.", True),
        BranchDefinition(PROVIDER_REJECTED, "Ashby conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Ashby returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
