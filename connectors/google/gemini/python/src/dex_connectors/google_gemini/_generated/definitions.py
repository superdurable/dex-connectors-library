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

CONNECTOR_ID = "gemini"
PROVIDER = "google"
GENERATE_CONTENT_OPERATION_ID = "generateContent"
BLOCKED = "blocked"
DEFECT = "defect"
GENERATED = "generated"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
TRUNCATED = "truncated"

GENERATE_CONTENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GENERATE_CONTENT_OPERATION_ID),
    (
        BranchDefinition(GENERATED, "Gemini finished the first candidate normally and returned its text.", False),
        BranchDefinition(TRUNCATED, "Gemini stopped the first candidate at the output token limit and returned any partial text.", True),
        BranchDefinition(BLOCKED, "Gemini blocked the prompt or stopped the candidate for safety, recitation, or another content policy.", True),
        BranchDefinition(PROVIDER_REJECTED, "Gemini conclusively rejected the request, such as an invalid argument, a denied permission, or an unknown model.", True),
        BranchDefinition(INVALID_RESPONSE, "Gemini returned a malformed, oversized, or unusable response, such as no candidate or an unrecognized finish reason.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=300),
        1,
        heartbeat_timeout=timedelta(seconds=300),
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=60), 3, timedelta(seconds=600)),
    ),
)
