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

CONNECTOR_ID = "gmail"
PROVIDER = "google"
GET_MESSAGE_OPERATION_ID = "getMessage"
SEND_MESSAGE_OPERATION_ID = "sendMessage"
REPLY_TO_MESSAGE_OPERATION_ID = "replyToMessage"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
READ = "read"
SENT = "sent"

GET_MESSAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(READ, "The Gmail message was read.", False),
        BranchDefinition(NOT_FOUND, "The Gmail message does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Gmail conclusively rejected the message query.", True),
        BranchDefinition(INVALID_RESPONSE, "Gmail returned an invalid or oversized message response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SEND_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "Gmail accepted the message; its message and thread IDs can be empty when an unusable accepted response has no Sent Mail match.", False),
        BranchDefinition(PROVIDER_REJECTED, "Gmail conclusively rejected the message.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

REPLY_TO_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, REPLY_TO_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "Gmail accepted the reply; its message ID can be empty when an unusable accepted response has no Sent Mail match, while the requested thread ID remains.", False),
        BranchDefinition(PROVIDER_REJECTED, "Gmail conclusively rejected the reply.", True),
        BranchDefinition(INVALID_RESPONSE, "Gmail returned an invalid or oversized source message response before sending the reply.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
