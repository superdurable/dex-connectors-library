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

CONNECTOR_ID = "microsoft-teams"
PROVIDER = "microsoft"
LIST_THREAD_REPLIES_OPERATION_ID = "listThreadReplies"
POST_CHANNEL_MESSAGE_OPERATION_ID = "postChannelMessage"
POST_THREAD_REPLY_OPERATION_ID = "postThreadReply"
POST_CHAT_MESSAGE_OPERATION_ID = "postChatMessage"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
READ = "read"
SENT = "sent"
UNCERTAIN = "uncertain"

LIST_THREAD_REPLIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_THREAD_REPLIES_OPERATION_ID),
    (
        BranchDefinition(READ, "One page of replies was read; it may be empty.", False),
        BranchDefinition(NOT_FOUND, "The team, channel, or message does not exist or the connected account cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the read, such as a missing ChannelMessage.Read.All administrator consent or an expired authorization.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized reply page, or a next-page link to another host.", True),
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
        BranchDefinition(SENT, "Teams stored the message, including one an earlier attempt of this Step execution sent and the read-back found.", False),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the message, such as an unknown or archived channel, a non-member account, or invalid HTML; nothing was posted.", True),
        BranchDefinition(UNCERTAIN, "An attempt sent the message and neither its response nor a read-back confirms it; it is never sent again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
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
        BranchDefinition(SENT, "Teams stored the message, including one an earlier attempt of this Step execution sent and the read-back found.", False),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the message, such as an unknown or archived channel, a non-member account, or invalid HTML; nothing was posted.", True),
        BranchDefinition(UNCERTAIN, "An attempt sent the message and neither its response nor a read-back confirms it; it is never sent again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

POST_CHAT_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, POST_CHAT_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "Teams stored the message in the chat.", False),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the message, such as an unknown chat, an account that left it, or invalid HTML; nothing was sent.", True),
        BranchDefinition(UNCERTAIN, "An attempt sent the message and its response does not confirm it; the connection cannot read chats, so it is never sent again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
