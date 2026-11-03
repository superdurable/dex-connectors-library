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

CONNECTOR_ID = "front"
PROVIDER = "front"
SEARCH_CONVERSATIONS_OPERATION_ID = "searchConversations"
GET_CONVERSATION_OPERATION_ID = "getConversation"
REPLY_TO_CONVERSATION_OPERATION_ID = "replyToConversation"
UPDATE_CONVERSATION_OPERATION_ID = "updateConversation"
FIND_CONTACT_BY_EMAIL_OPERATION_ID = "findContactByEmail"
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
        BranchDefinition(SEARCHED, "Front returned one page of matching conversations; it may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Front conclusively rejected the search, such as an invalid token, a token without conversation read access, or an expired page token.", True),
        BranchDefinition(INVALID_RESPONSE, "Front returned a malformed or oversized page, or a next-page link outside Front's API hosts.", True),
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
        BranchDefinition(FOUND, "Front returned the conversation with its newest messages and comments.", False),
        BranchDefinition(MERGED, "The conversation was merged into another; mergedIntoConversationId names the conversation to read instead.", True),
        BranchDefinition(NOT_FOUND, "The conversation does not exist or is not visible to the API token.", True),
        BranchDefinition(PROVIDER_REJECTED, "Front conclusively rejected the read, such as an invalid token or a token without message or comment read access.", True),
        BranchDefinition(INVALID_RESPONSE, "Front returned a malformed or oversized conversation, message page, or comment list.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

REPLY_TO_CONVERSATION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, REPLY_TO_CONVERSATION_OPERATION_ID),
    (
        BranchDefinition(REPLIED, "Front accepted the reply for delivery or created the comment.", False),
        BranchDefinition(NOT_FOUND, "The conversation does not exist, was merged, or is not visible to the API token; nothing was sent.", True),
        BranchDefinition(PROVIDER_REJECTED, "Front conclusively rejected the reply or comment, such as a token without send permission or an unknown author; nothing was sent.", True),
        BranchDefinition(UNCERTAIN, "The reply or comment may exist after a lost answer, ambiguous server error, late answer, 409 conflict, or terminal credential failure during accepted-write read-back; an accepted comment may have been deleted before read-back. The connector never resends without proof of non-application.", True),
        BranchDefinition(DEFECT, "Local input, connection credentials that require reauthorization or are unusable before sending, or the connector definition is invalid; transient credential failures Retry.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)

UPDATE_CONVERSATION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_CONVERSATION_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The conversation holds the requested status, assignee, and tags, written now or by an earlier attempt.", False),
        BranchDefinition(NOT_FOUND, "The conversation does not exist, was merged, or is not visible to the API token.", True),
        BranchDefinition(PROVIDER_REJECTED, "Front conclusively rejected a change, such as an unknown teammate, tag, or ticket status; changes sent before it stay applied.", True),
        BranchDefinition(INVALID_RESPONSE, "Front returned a malformed or oversized conversation, so the change may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=60),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

FIND_CONTACT_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_CONTACT_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "A Front contact has the email handle.", False),
        BranchDefinition(NOT_FOUND, "No Front contact has the email handle.", True),
        BranchDefinition(PROVIDER_REJECTED, "Front conclusively rejected the lookup, such as an invalid token or a token without contact read access.", True),
        BranchDefinition(INVALID_RESPONSE, "Front returned a malformed or oversized contact.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
