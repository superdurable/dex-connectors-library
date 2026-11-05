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

CONNECTOR_ID = "twilio-messaging"
PROVIDER = "twilio"
SEND_MESSAGE_OPERATION_ID = "sendMessage"
GET_MESSAGE_OPERATION_ID = "getMessage"
ACCEPTED = "accepted"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"

SEND_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(ACCEPTED, "Twilio created the message and returned its SID; delivery status arrives later through getMessage.", False),
        BranchDefinition(PROVIDER_REJECTED, "Twilio conclusively rejected the message, such as an invalid To number, an unverified trial recipient, an unsubscribed recipient, or an unusable sender; nothing was sent.", True),
        BranchDefinition(UNCERTAIN, "The request was dispatched but its outcome cannot be confirmed; Twilio has no idempotency key, so the message may exist and must not be resent automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

GET_MESSAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The message exists and its current status was read.", False),
        BranchDefinition(NOT_FOUND, "The account has no message with that SID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Twilio conclusively rejected the message query.", True),
        BranchDefinition(INVALID_RESPONSE, "Twilio returned an invalid or oversized message response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
