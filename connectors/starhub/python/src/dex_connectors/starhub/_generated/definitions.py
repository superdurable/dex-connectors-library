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

CONNECTOR_ID = "starhub"
PROVIDER = "starhub"
LIST_MODELS_OPERATION_ID = "listModels"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_MODELS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MODELS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "StarHub Ai returned the available model list.", False),
        BranchDefinition(PROVIDER_REJECTED, "StarHub Ai rejected the Key or request.", True),
        BranchDefinition(INVALID_RESPONSE, "StarHub Ai returned malformed or oversized model data.", True),
        BranchDefinition(DEFECT, "The local API Key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
