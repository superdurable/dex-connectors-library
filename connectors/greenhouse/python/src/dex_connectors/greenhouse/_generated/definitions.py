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

CONNECTOR_ID = "greenhouse"
PROVIDER = "greenhouse"
GET_CANDIDATE_OPERATION_ID = "getCandidate"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_CANDIDATE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CANDIDATE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Greenhouse returned the candidate.", False),
        BranchDefinition(NOT_FOUND, "Greenhouse returned no accessible candidate for this ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Greenhouse conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Greenhouse returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
