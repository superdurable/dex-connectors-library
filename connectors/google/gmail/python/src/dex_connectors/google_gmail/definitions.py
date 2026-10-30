# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Stable operation identities and branch contracts from connector.yaml."""

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

GET_MESSAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, "getMessage"),
    (
        BranchDefinition("read", "The Gmail message was read."),
        BranchDefinition("notFound", "The Gmail message does not exist.", True),
        BranchDefinition("providerRejected", "Gmail conclusively rejected the message query.", True),
        BranchDefinition("invalidResponse", "Gmail returned an invalid or oversized message response.", True),
        BranchDefinition(
            "defect", "Local input, connection configuration, or connector definition is invalid.", True
        ),
    ),
    StepDefaults(timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()),
)

_SEND_RETRY = RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(minutes=2))

SEND_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, "sendMessage"),
    (
        BranchDefinition(
            "sent",
            "Gmail accepted the message; its message and thread IDs can be empty when an unusable "
            "accepted response has no Sent Mail match.",
        ),
        BranchDefinition("providerRejected", "Gmail conclusively rejected the message.", True),
        BranchDefinition(
            "defect", "Local input, connection configuration, or connector definition is invalid.", True
        ),
    ),
    StepDefaults(timedelta(seconds=30), 2, execute_retry=_SEND_RETRY),
)

REPLY_TO_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, "replyToMessage"),
    (
        BranchDefinition(
            "sent",
            "Gmail accepted the reply; its message ID can be empty when an unusable accepted "
            "response has no Sent Mail match, while the requested thread ID remains.",
        ),
        BranchDefinition("providerRejected", "Gmail conclusively rejected the reply.", True),
        BranchDefinition(
            "invalidResponse",
            "Gmail returned an invalid or oversized source message response before sending the reply.",
            True,
        ),
        BranchDefinition(
            "defect", "Local input, connection configuration, or connector definition is invalid.", True
        ),
    ),
    StepDefaults(timedelta(seconds=30), 2, execute_retry=_SEND_RETRY),
)
