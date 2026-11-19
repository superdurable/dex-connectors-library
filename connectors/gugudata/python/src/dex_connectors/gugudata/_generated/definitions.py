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

CONNECTOR_ID = "gugudata"
PROVIDER = "gugudata"
SEGMENT_TEXT_OPERATION_ID = "segmentText"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
SEGMENTED = "segmented"

SEGMENT_TEXT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEGMENT_TEXT_OPERATION_ID),
    (
        BranchDefinition(SEGMENTED, "GuGuData returned segmented text.", False),
        BranchDefinition(PROVIDER_REJECTED, "GuGuData rejected the key, permission, quota, or text.", True),
        BranchDefinition(INVALID_RESPONSE, "GuGuData returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
