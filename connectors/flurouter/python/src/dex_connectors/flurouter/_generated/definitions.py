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

CONNECTOR_ID = "flurouter"
PROVIDER = "flurouter"
LIST_MODELS_OPERATION_ID = "listModels"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_MODELS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MODELS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "FluRouter returned the Key-visible model list.", False),
        BranchDefinition(PROVIDER_REJECTED, "FluRouter rejected the Key or request.", True),
        BranchDefinition(INVALID_RESPONSE, "FluRouter returned malformed or oversized model data.", True),
        BranchDefinition(DEFECT, "The local API Key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
