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

CONNECTOR_ID = "heap"
PROVIDER = "heap"
SET_USER_PROPERTIES_OPERATION_ID = "setUserProperties"
ACCEPTED = "accepted"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

SET_USER_PROPERTIES_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SET_USER_PROPERTIES_OPERATION_ID),
    (
        BranchDefinition(ACCEPTED, "Heap accepted the property updates.", False),
        BranchDefinition(PROVIDER_REJECTED, "Heap conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Heap returned an oversized or unreadable confirmation.", True),
        BranchDefinition(DEFECT, "Local identity, property input, or app ID is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
