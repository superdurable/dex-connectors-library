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

CONNECTOR_ID = "douyin"
PROVIDER = "douyin"
GET_BEHAVIOR_SUMMARY_OPERATION_ID = "getBehaviorSummary"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_BEHAVIOR_SUMMARY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_BEHAVIOR_SUMMARY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Douyin returned behavior summary counts.", False),
        BranchDefinition(PROVIDER_REJECTED, "Douyin rejected credentials or report request.", True),
        BranchDefinition(INVALID_RESPONSE, "Douyin returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local date or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
