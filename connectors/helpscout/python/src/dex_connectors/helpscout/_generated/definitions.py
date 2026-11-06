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

CONNECTOR_ID = "helpscout"
PROVIDER = "helpscout"
SEARCH_CONVERSATIONS_OPERATION_ID = "searchConversations"
GET_CONVERSATION_OPERATION_ID = "getConversation"
REPLY_TO_CONVERSATION_OPERATION_ID = "replyToConversation"
UPDATE_CONVERSATION_OPERATION_ID = "updateConversation"
FIND_CUSTOMER_BY_EMAIL_OPERATION_ID = "findCustomerByEmail"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MERGED = "merged"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
REPLIED = "replied"
SEARCHED = "searched"
UNCERTAIN = "uncertain"
UPDATED = "updated"

SEARCH_CONVERSATIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_CONVERSATIONS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "Help Scout returned one page of matching conversations; it may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Help Scout conclusively rejected the search, such as an invalid token, a filter it does not accept, or an account without API access.", True),
        BranchDefinition(INVALID_RESPONSE, "Help Scout returned a malformed or oversized page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_CONVERSATION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CONVERSATION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Help Scout returned the conversation and its newest threads.", False),
        BranchDefinition(MERGED, "The conversation was merged into another; mergedIntoConversationId names the conversation to read instead.", True),
        BranchDefinition(NOT_FOUND, "The conversation does not exist, was deleted, or is not visible to the app's Help Scout user.", True),
        BranchDefinition(PROVIDER_REJECTED, "Help Scout conclusively rejected the read, such as an invalid token or an account without API access.", True),
        BranchDefinition(INVALID_RESPONSE, "Help Scout returned a malformed or oversized conversation or thread page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

REPLY_TO_CONVERSATION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, REPLY_TO_CONVERSATION_OPERATION_ID),
    (
        BranchDefinition(REPLIED, "Help Scout created the reply or note and returned its thread ID.", False),
        BranchDefinition(NOT_FOUND, "The conversation does not exist, was merged, or is not visible to the app's Help Scout user; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Help Scout conclusively rejected the thread, such as a locked conversation with 100 threads or an unknown customer; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "The thread was sent but Help Scout's answer was lost, a server error, or unreadable, so it may exist; the connector never sends it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_CONVERSATION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_CONVERSATION_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The conversation holds the requested status, assignee, and tags, written now or by an earlier attempt.", False),
        BranchDefinition(NOT_FOUND, "The conversation does not exist, was merged, or is not visible to the app's Help Scout user.", True),
        BranchDefinition(PROVIDER_REJECTED, "Help Scout conclusively rejected the change, such as an assignee who is not a user or team of the account.", True),
        BranchDefinition(INVALID_RESPONSE, "Help Scout returned a malformed or oversized conversation, so part of the change may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

FIND_CUSTOMER_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_CUSTOMER_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Help Scout returned one or more customer profiles for the email address.", False),
        BranchDefinition(NOT_FOUND, "No customer profile has the email address.", True),
        BranchDefinition(PROVIDER_REJECTED, "Help Scout conclusively rejected the lookup, such as an invalid token or an account without API access.", True),
        BranchDefinition(INVALID_RESPONSE, "Help Scout returned a malformed or oversized customer page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
