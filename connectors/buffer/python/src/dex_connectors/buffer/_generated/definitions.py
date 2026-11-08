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

CONNECTOR_ID = "buffer"
PROVIDER = "buffer"
GET_CHANNEL_OPERATION_ID = "getChannel"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_CHANNEL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CHANNEL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Buffer returned the channel.", False),
        BranchDefinition(NOT_FOUND, "Buffer did not expose a channel for this ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Buffer returned a definite GraphQL or HTTP rejection.", True),
        BranchDefinition(INVALID_RESPONSE, "Buffer returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
