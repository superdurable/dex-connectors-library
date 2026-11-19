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

CONNECTOR_ID = "zhipu"
PROVIDER = "zhipu"
GENERATE_TEXT_OPERATION_ID = "generateText"
BLOCKED = "blocked"
DEFECT = "defect"
GENERATED = "generated"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
TRUNCATED = "truncated"

GENERATE_TEXT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GENERATE_TEXT_OPERATION_ID),
    (
        BranchDefinition(GENERATED, "The model returned a complete text response.", False),
        BranchDefinition(TRUNCATED, "The model stopped at the token limit and returned partial text.", True),
        BranchDefinition(BLOCKED, "The model refused or content filtering prevented a text answer.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zhipu rejected the key, quota, model, or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Zhipu returned malformed, oversized, or unsupported content.", True),
        BranchDefinition(DEFECT, "Local request or connection settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=180),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=3600)),
    ),
)
