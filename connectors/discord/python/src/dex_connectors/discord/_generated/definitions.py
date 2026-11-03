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

CONNECTOR_ID = "discord"
PROVIDER = "discord"
GET_CHANNEL_OPERATION_ID = "getChannel"
LIST_CHANNEL_MESSAGES_OPERATION_ID = "listChannelMessages"
CREATE_MESSAGE_OPERATION_ID = "createMessage"
ADD_REACTION_OPERATION_ID = "addReaction"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_CHANNEL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CHANNEL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The channel exists and the bot can view it.", False),
        BranchDefinition(NOT_FOUND, "Discord knows no channel with the ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Discord conclusively rejected the read, such as a channel the bot cannot view or a rejected token.", True),
        BranchDefinition(INVALID_RESPONSE, "Discord returned an invalid or oversized channel response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_CHANNEL_MESSAGES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CHANNEL_MESSAGES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Discord returned one bounded page of messages; the page may be empty.", False),
        BranchDefinition(NOT_FOUND, "Discord knows no channel with the ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Discord conclusively rejected the listing, such as a missing Read Message History permission or a rejected token.", True),
        BranchDefinition(INVALID_RESPONSE, "Discord returned an invalid or oversized message page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Discord created the message, or returned the message an earlier attempt created with the same nonce.", False),
        BranchDefinition(NOT_FOUND, "Discord knows no channel with the ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Discord conclusively rejected the message, such as a missing Send Messages permission, invalid content, or a rejected token.", True),
        BranchDefinition(INVALID_RESPONSE, "Discord applied or rejected the message but returned an invalid or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

ADD_REACTION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_REACTION_OPERATION_ID),
    (
        BranchDefinition(ADDED, "The message carries the bot's reaction with the emoji.", False),
        BranchDefinition(NOT_FOUND, "Discord knows no such channel, message, or custom emoji.", True),
        BranchDefinition(PROVIDER_REJECTED, "Discord conclusively rejected the reaction, such as a missing Add Reactions permission, a reaction limit, or a rejected token.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
