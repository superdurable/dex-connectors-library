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

CONNECTOR_ID = "amazon-sqs"
PROVIDER = "amazon-sqs"
RECEIVE_MESSAGES_OPERATION_ID = "receiveMessages"
DELETE_MESSAGE_OPERATION_ID = "deleteMessage"
CHANGE_MESSAGE_VISIBILITY_OPERATION_ID = "changeMessageVisibility"
CHANGED = "changed"
DEFECT = "defect"
DELETED = "deleted"
EMPTY = "empty"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
RECEIPT_HANDLE_INVALID = "receiptHandleInvalid"
RECEIVED = "received"

RECEIVE_MESSAGES_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, RECEIVE_MESSAGES_OPERATION_ID),
    (
        BranchDefinition(RECEIVED, "SQS returned at least one message.", False),
        BranchDefinition(EMPTY, "No message arrived within the wait time.", True),
        BranchDefinition(PROVIDER_REJECTED, "SQS conclusively rejected the receive, such as a missing queue, a wrong Region, invalid credentials, missing sqs:ReceiveMessage or kms:Decrypt permission, or too many in-flight messages.", True),
        BranchDefinition(INVALID_RESPONSE, "SQS returned an invalid or oversized batch, or a message whose body does not match its MD5 digest; the batch's messages reappear when their visibility timeout ends.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

DELETE_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, DELETE_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(DELETED, "SQS accepted the deletion. Amazon SQS also accepts an outdated receipt handle from an earlier delivery without deleting the message, which is then delivered again.", False),
        BranchDefinition(RECEIPT_HANDLE_INVALID, "SQS no longer accepts the receipt handle, such as an expired FIFO receipt handle or a malformed one, or, on a compatible server such as ElasticMQ, a handle whose message an earlier attempt already deleted; the message may be delivered again.", True),
        BranchDefinition(PROVIDER_REJECTED, "SQS conclusively rejected the deletion, such as a missing queue, a wrong Region, invalid credentials, or missing sqs:DeleteMessage permission.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CHANGE_MESSAGE_VISIBILITY_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CHANGE_MESSAGE_VISIBILITY_OPERATION_ID),
    (
        BranchDefinition(CHANGED, "SQS set the message's visibility timeout.", False),
        BranchDefinition(RECEIPT_HANDLE_INVALID, "SQS no longer accepts the receipt handle, because the message is no longer in flight, was deleted, or the handle is expired or malformed.", True),
        BranchDefinition(PROVIDER_REJECTED, "SQS conclusively rejected the change, such as a missing queue, a wrong Region, invalid credentials, missing sqs:ChangeMessageVisibility permission, or a timeout above the 12-hour limit.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
