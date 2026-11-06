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

CONNECTOR_ID = "outlook-mail"
PROVIDER = "microsoft"
SEARCH_MESSAGES_OPERATION_ID = "searchMessages"
GET_MESSAGE_OPERATION_ID = "getMessage"
SEND_MESSAGE_OPERATION_ID = "sendMessage"
REPLY_TO_MESSAGE_OPERATION_ID = "replyToMessage"
MOVE_MESSAGE_OPERATION_ID = "moveMessage"
SET_MESSAGE_FLAGS_OPERATION_ID = "setMessageFlags"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MOVED = "moved"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"
SENT = "sent"
UNCERTAIN = "uncertain"
UPDATED = "updated"

SEARCH_MESSAGES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_MESSAGES_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "Microsoft Graph returned one page of matching summaries, newest first; it may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the search, such as a revoked authorization, a folder that does not exist, a mailbox the app may not read, or a filter it does not accept.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned a malformed or oversized page, or a next-page link outside graph.microsoft.com.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_MESSAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The message was read.", False),
        BranchDefinition(NOT_FOUND, "No message with the ID exists in the mailbox, for example because it was deleted or moved to an archive mailbox.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the read, such as a revoked authorization or a mailbox the app may not read.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned a malformed or oversized message or attachment list.", True),
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
        BranchDefinition(SENT, "Microsoft Graph accepted the draft for sending, now or in an earlier attempt of this Step execution that this attempt confirmed.", False),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively refused the draft or the send, such as a revoked authorization, a mailbox without send permission, or an invalid recipient, so nothing was sent.", True),
        BranchDefinition(UNCERTAIN, "The send was dispatched but its answer was lost and the draft has not appeared as sent, so the message may have been sent; the connector never sends it again, and the Result names the draft to inspect.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=90),
        1,
        heartbeat_timeout=timedelta(seconds=90),
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 8, timedelta(seconds=600)),
    ),
)

REPLY_TO_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, REPLY_TO_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "Microsoft Graph accepted the reply draft for sending, now or in an earlier attempt of this Step execution that this attempt confirmed.", False),
        BranchDefinition(NOT_FOUND, "No message with the ID exists in the mailbox, so no reply was drafted or sent.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively refused the reply draft or the send, such as a revoked authorization, a mailbox without send permission, or an original without a usable reply address, so nothing was sent.", True),
        BranchDefinition(UNCERTAIN, "The send was dispatched but its answer was lost and the draft has not appeared as sent, so the reply may have been sent; the connector never sends it again, and the Result names the draft to inspect.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=90),
        1,
        heartbeat_timeout=timedelta(seconds=90),
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 8, timedelta(seconds=600)),
    ),
)

MOVE_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, MOVE_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(MOVED, "The message is in the destination folder, moved now or by an earlier attempt.", False),
        BranchDefinition(NOT_FOUND, "No message with the ID exists in the mailbox.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively refused the move, such as a destination folder that does not exist, a revoked authorization, or a read-only mailbox.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SET_MESSAGE_FLAGS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SET_MESSAGE_FLAGS_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The message holds the requested values, set now or by an earlier attempt.", False),
        BranchDefinition(NOT_FOUND, "No message with the ID exists in the mailbox.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively refused the change, such as a revoked authorization or a read-only mailbox.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
