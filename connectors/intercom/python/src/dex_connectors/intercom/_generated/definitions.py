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

CONNECTOR_ID = "intercom"
PROVIDER = "intercom"
SEARCH_CONVERSATIONS_OPERATION_ID = "searchConversations"
GET_CONVERSATION_OPERATION_ID = "getConversation"
REPLY_TO_CONVERSATION_OPERATION_ID = "replyToConversation"
UPDATE_CONVERSATION_STATE_OPERATION_ID = "updateConversationState"
FIND_CONTACT_BY_EMAIL_OPERATION_ID = "findContactByEmail"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
REPLIED = "replied"
SEARCHED = "searched"
UNCERTAIN = "uncertain"
UPDATED = "updated"

SEARCH_CONVERSATIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_CONVERSATIONS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of matching conversations was returned; it may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Intercom conclusively rejected the search, such as an invalid token, a missing permission, or an invalid cursor.", True),
        BranchDefinition(INVALID_RESPONSE, "Intercom returned an invalid or oversized search page.", True),
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
        BranchDefinition(FOUND, "The conversation and its latest parts were read.", False),
        BranchDefinition(NOT_FOUND, "The conversation does not exist or is not visible to the connection's token.", True),
        BranchDefinition(PROVIDER_REJECTED, "Intercom conclusively rejected the conversation read.", True),
        BranchDefinition(INVALID_RESPONSE, "Intercom returned an invalid or oversized conversation.", True),
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
        BranchDefinition(REPLIED, "The reply or note is in the conversation; a retried attempt that found its own earlier reply reports it instead of sending again.", False),
        BranchDefinition(NOT_FOUND, "The conversation does not exist or is not visible to the connection's token; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Intercom conclusively rejected the reply, such as an unknown admin or a closed channel; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "An earlier attempt sent the reply but its outcome cannot be confirmed and the conversation shows no matching part, so the reply may exist; the connector never resends it.", True),
        BranchDefinition(INVALID_RESPONSE, "Intercom accepted the reply but returned an invalid or oversized conversation, so the reply exists.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)

UPDATE_CONVERSATION_STATE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_CONVERSATION_STATE_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The conversation is in the requested state; a repeated attempt that finds it there writes nothing.", False),
        BranchDefinition(NOT_FOUND, "The conversation does not exist or is not visible to the connection's token.", True),
        BranchDefinition(PROVIDER_REJECTED, "Intercom conclusively rejected the change, such as an unknown admin, and the conversation is not in the requested state.", True),
        BranchDefinition(INVALID_RESPONSE, "Intercom returned an invalid or oversized conversation, so the change may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

FIND_CONTACT_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_CONTACT_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "At least one contact has the email address.", False),
        BranchDefinition(NOT_FOUND, "No contact has the email address yet; Intercom can take a few minutes to index a new contact.", True),
        BranchDefinition(PROVIDER_REJECTED, "Intercom conclusively rejected the contact search.", True),
        BranchDefinition(INVALID_RESPONSE, "Intercom returned an invalid or oversized contact page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
