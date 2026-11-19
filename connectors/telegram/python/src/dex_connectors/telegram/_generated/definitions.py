# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RepeatOnUnknown,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "telegram"
PROVIDER = "telegram"
SEND_MESSAGE_OPERATION_ID = "sendMessage"
GET_CHAT_OPERATION_ID = "getChat"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SENT = "sent"
UNCERTAIN = "uncertain"

SEND_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "Telegram created the message and returned its ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "Telegram conclusively rejected the send before creating a message.", True),
        BranchDefinition(UNCERTAIN, "Telegram may have created the message, but the send cap was reached or Telegram accepted it without a complete confirmation; check the chat before sending again.", True),
        BranchDefinition(DEFECT, "Local input, bot token, or dispatch fence is invalid before any send.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.LIMITED,
    max_sends=3,
)

GET_CHAT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CHAT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Telegram returned the chat metadata.", False),
        BranchDefinition(NOT_FOUND, "Telegram could not find the chat.", True),
        BranchDefinition(PROVIDER_REJECTED, "Telegram rejected the chat lookup or bot credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Telegram returned malformed or oversized chat data.", True),
        BranchDefinition(DEFECT, "Local input or bot token is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
