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

CONNECTOR_ID = "iflytek"
PROVIDER = "iflytek"
CREATE_CHAT_COMPLETION_OPERATION_ID = "createChatCompletion"
COMPLETED = "completed"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

CREATE_CHAT_COMPLETION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, CREATE_CHAT_COMPLETION_OPERATION_ID),
    (
        BranchDefinition(COMPLETED, "Spark returned one assistant text answer.", False),
        BranchDefinition(PROVIDER_REJECTED, "Spark conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Spark returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local prompt, model, or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=60),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=20), 3, timedelta(seconds=300)),
    ),
)
