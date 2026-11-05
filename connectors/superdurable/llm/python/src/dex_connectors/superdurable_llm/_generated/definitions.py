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

CONNECTOR_ID = "llm"
PROVIDER = "llm"
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
        BranchDefinition(GENERATED, "The model finished normally and returned text.", False),
        BranchDefinition(TRUNCATED, "The model stopped at the output token limit or the context window and returned any partial text.", True),
        BranchDefinition(BLOCKED, "The provider stopped the request or response for a content policy, or the model refused.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request, such as an invalid key, an unknown model, or exhausted quota or balance.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned a malformed, oversized, or unusable response, including structured output that does not match its schema.", True),
        BranchDefinition(DEFECT, "Local input, a request field the model does not accept, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=1200),
        1,
        heartbeat_timeout=timedelta(seconds=60),
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=60), 3, timedelta(seconds=3600)),
    ),
)
