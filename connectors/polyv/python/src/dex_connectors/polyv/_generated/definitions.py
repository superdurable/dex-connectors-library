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

CONNECTOR_ID = "polyv"
PROVIDER = "polyv"
LIST_CHANNELS_OPERATION_ID = "listChannels"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_CHANNELS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CHANNELS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Polyv returned channel IDs.", False),
        BranchDefinition(PROVIDER_REJECTED, "Polyv rejected the signed request.", True),
        BranchDefinition(INVALID_RESPONSE, "Polyv returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials are unavailable or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
