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

CONNECTOR_ID = "cohere"
PROVIDER = "cohere"
EMBED_OPERATION_ID = "embed"
RERANK_OPERATION_ID = "rerank"
DEFECT = "defect"
EMBEDDED = "embedded"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
RANKED = "ranked"

EMBED_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, EMBED_OPERATION_ID),
    (
        BranchDefinition(EMBEDDED, "Cohere returned one float vector per text.", False),
        BranchDefinition(PROVIDER_REJECTED, "Cohere conclusively rejected the embedding request.", True),
        BranchDefinition(INVALID_RESPONSE, "Cohere returned malformed or oversized embedding data.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)

RERANK_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, RERANK_OPERATION_ID),
    (
        BranchDefinition(RANKED, "Cohere returned ranked document indices and scores.", False),
        BranchDefinition(PROVIDER_REJECTED, "Cohere conclusively rejected the rerank request.", True),
        BranchDefinition(INVALID_RESPONSE, "Cohere returned malformed or oversized rerank data.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
