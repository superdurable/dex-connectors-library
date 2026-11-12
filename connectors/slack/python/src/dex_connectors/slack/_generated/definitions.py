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

CONNECTOR_ID = "slack"
PROVIDER = "slack"
LIST_CHANNELS_OPERATION_ID = "listChannels"
GET_WORKSPACE_IDENTITY_OPERATION_ID = "getWorkspaceIdentity"
LIST_THREAD_MESSAGES_OPERATION_ID = "listThreadMessages"
GET_THREAD_REPLY_OPERATION_ID = "getThreadReply"
POST_CHANNEL_MESSAGE_OPERATION_ID = "postChannelMessage"
POST_THREAD_REPLY_OPERATION_ID = "postThreadReply"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
READ = "read"
SENT = "sent"
UNCERTAIN = "uncertain"

LIST_CHANNELS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CHANNELS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "The channel page was read.", False),
        BranchDefinition(PROVIDER_REJECTED, "Slack conclusively rejected the channel query.", True),
        BranchDefinition(INVALID_RESPONSE, "Slack returned an invalid or oversized channel response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_WORKSPACE_IDENTITY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_WORKSPACE_IDENTITY_OPERATION_ID),
    (
        BranchDefinition(READ, "The workspace and bot identity were read.", False),
        BranchDefinition(PROVIDER_REJECTED, "Slack conclusively rejected the identity query.", True),
        BranchDefinition(INVALID_RESPONSE, "Slack returned an invalid or oversized identity response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_THREAD_MESSAGES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_THREAD_MESSAGES_OPERATION_ID),
    (
        BranchDefinition(READ, "The thread page was read.", False),
        BranchDefinition(PROVIDER_REJECTED, "Slack conclusively rejected the thread query.", True),
        BranchDefinition(INVALID_RESPONSE, "Slack returned an invalid or oversized thread response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_THREAD_REPLY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_THREAD_REPLY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The reply was found.", False),
        BranchDefinition(NOT_FOUND, "The reply does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Slack conclusively rejected the reply query.", True),
        BranchDefinition(INVALID_RESPONSE, "Slack returned an invalid or oversized reply response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

POST_CHANNEL_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, POST_CHANNEL_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "Slack accepted the message.", False),
        BranchDefinition(PROVIDER_REJECTED, "Slack conclusively rejected the message.", True),
        BranchDefinition(UNCERTAIN, "The dispatched message outcome cannot be confirmed.", True),
        BranchDefinition(DEFECT, "Local input, terminal connection credentials, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

POST_THREAD_REPLY_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, POST_THREAD_REPLY_OPERATION_ID),
    (
        BranchDefinition(SENT, "Slack accepted the message.", False),
        BranchDefinition(PROVIDER_REJECTED, "Slack conclusively rejected the message.", True),
        BranchDefinition(UNCERTAIN, "The dispatched message outcome cannot be confirmed.", True),
        BranchDefinition(DEFECT, "Local input, terminal connection credentials, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
