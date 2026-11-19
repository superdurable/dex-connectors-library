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

CONNECTOR_ID = "tokengp"
PROVIDER = "tokengp"
LIST_MODELS_OPERATION_ID = "listModels"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_MODELS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MODELS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "TokenGP returned accessible model IDs.", False),
        BranchDefinition(PROVIDER_REJECTED, "TokenGP rejected the API key or account access.", True),
        BranchDefinition(INVALID_RESPONSE, "TokenGP returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or connection configuration are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
