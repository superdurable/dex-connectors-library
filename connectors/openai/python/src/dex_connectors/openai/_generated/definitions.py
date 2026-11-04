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

CONNECTOR_ID = "openai"
PROVIDER = "openai"
CREATE_RESPONSE_OPERATION_ID = "createResponse"
RETRIEVE_RESPONSE_OPERATION_ID = "retrieveResponse"
COMPLETED = "completed"
DEFECT = "defect"
FAILED = "failed"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"

CREATE_RESPONSE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_RESPONSE_OPERATION_ID),
    (
        BranchDefinition(COMPLETED, "OpenAI completed the response.", False),
        BranchDefinition(FAILED, "OpenAI returned a terminal failed or incomplete response. A cancelled response is not failed: the next attempt creates a new one.", True),
        BranchDefinition(PROVIDER_REJECTED, "OpenAI conclusively rejected the response request.", True),
        BranchDefinition(UNCERTAIN, "A request may have created a response that cannot be confirmed: no response ID was observed or checkpointed, for example after HTTP 408 or 409, a 5xx other than 503, or a streamed error event, or the stored response cannot be read back. The Responses API documents no deduplication of create requests.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=150),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)

RETRIEVE_RESPONSE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, RETRIEVE_RESPONSE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The response exists and was returned.", False),
        BranchDefinition(NOT_FOUND, "The requested response does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "OpenAI conclusively rejected the response query.", True),
        BranchDefinition(INVALID_RESPONSE, "OpenAI returned an invalid or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
