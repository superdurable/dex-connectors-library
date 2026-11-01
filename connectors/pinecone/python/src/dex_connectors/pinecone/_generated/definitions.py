# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation identities, branches, and Step defaults."""

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

CONNECTOR_ID = "pinecone"
PROVIDER = "pinecone"
UPSERT_VECTORS_OPERATION_ID = "upsertVectors"
QUERY_VECTORS_OPERATION_ID = "queryVectors"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
QUERIED = "queried"
UPSERTED = "upserted"

UPSERT_VECTORS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPSERT_VECTORS_OPERATION_ID),
    (
        BranchDefinition(UPSERTED, "Upsert vector IDs by absolute replacement in one namespace.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input, configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

QUERY_VECTORS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_VECTORS_OPERATION_ID),
    (
        BranchDefinition(QUERIED, "Find nearest vectors in one namespace.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input, configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
