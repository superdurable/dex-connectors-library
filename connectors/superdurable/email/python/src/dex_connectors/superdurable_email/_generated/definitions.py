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

CONNECTOR_ID = "email"
PROVIDER = "email"
SEARCH_MESSAGES_OPERATION_ID = "searchMessages"
GET_MESSAGE_OPERATION_ID = "getMessage"
SEND_MESSAGE_OPERATION_ID = "sendMessage"
REPLY_TO_MESSAGE_OPERATION_ID = "replyToMessage"
MOVE_MESSAGE_OPERATION_ID = "moveMessage"
SET_FLAGS_OPERATION_ID = "setFlags"
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
        BranchDefinition(SEARCHED, "The search ran; the Result holds up to limit matching summaries, newest first, and may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "The IMAP server conclusively refused the login, TLS, mailbox, or search, such as a wrong app password, a certificate that fails verification, or a mailbox that does not exist.", True),
        BranchDefinition(INVALID_RESPONSE, "The IMAP server returned a response the connector cannot parse.", True),
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
        BranchDefinition(NOT_FOUND, "The mailbox does not exist, its UIDVALIDITY changed, or no message has the UID.", True),
        BranchDefinition(PROVIDER_REJECTED, "The IMAP server conclusively refused the login, TLS, or read.", True),
        BranchDefinition(INVALID_RESPONSE, "The IMAP server returned a response or message the connector cannot parse.", True),
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
        BranchDefinition(SENT, "The SMTP server accepted the message for delivery.", False),
        BranchDefinition(PROVIDER_REJECTED, "The SMTP server conclusively refused the login, TLS, sender, a recipient, or the message, so nothing was sent.", True),
        BranchDefinition(UNCERTAIN, "The message was submitted but the server's answer was lost, so it may have been sent; the connector never submits it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=150),
        1,
        heartbeat_timeout=timedelta(seconds=150),
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=60), 5, timedelta(seconds=600)),
    ),
)

REPLY_TO_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, REPLY_TO_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "The SMTP server accepted the reply for delivery.", False),
        BranchDefinition(NOT_FOUND, "The source mailbox does not exist, its UIDVALIDITY changed, or no message has the UID, so nothing was sent.", True),
        BranchDefinition(PROVIDER_REJECTED, "The IMAP or SMTP server conclusively refused the login, TLS, read, sender, a recipient, or the reply, so nothing was sent.", True),
        BranchDefinition(INVALID_RESPONSE, "The source message has no usable reply address or could not be parsed, so nothing was sent.", True),
        BranchDefinition(UNCERTAIN, "The reply was submitted but the server's answer was lost, so it may have been sent; the connector never submits it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=150),
        1,
        heartbeat_timeout=timedelta(seconds=150),
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=60), 5, timedelta(seconds=600)),
    ),
)

MOVE_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, MOVE_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(MOVED, "The message is in the destination mailbox, moved now or by an earlier attempt.", False),
        BranchDefinition(NOT_FOUND, "The source mailbox does not exist, its UIDVALIDITY changed, or no message has the UID and none with the expected Message-ID is in the destination.", True),
        BranchDefinition(PROVIDER_REJECTED, "The IMAP server conclusively refused the login, TLS, or move, such as a destination mailbox that does not exist or a server without MOVE or UIDPLUS.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SET_FLAGS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SET_FLAGS_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The message holds the requested flags, set now or by an earlier attempt.", False),
        BranchDefinition(NOT_FOUND, "The mailbox does not exist, its UIDVALIDITY changed, or no message has the UID.", True),
        BranchDefinition(PROVIDER_REJECTED, "The IMAP server conclusively refused the login, TLS, or flag change, such as a read-only mailbox.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
