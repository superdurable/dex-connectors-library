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

CONNECTOR_ID = "reamaze"
PROVIDER = "reamaze"
SEARCH_CONVERSATIONS_OPERATION_ID = "searchConversations"
GET_CONVERSATION_OPERATION_ID = "getConversation"
CREATE_CONVERSATION_OPERATION_ID = "createConversation"
UPDATE_CONVERSATION_OPERATION_ID = "updateConversation"
REPLY_TO_CONVERSATION_OPERATION_ID = "replyToConversation"
FIND_CONTACT_BY_EMAIL_OPERATION_ID = "findContactByEmail"
CREATED = "created"
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
        BranchDefinition(SEARCHED, "Re:amaze returned one page of matching conversations; it may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Re:amaze conclusively rejected the search, such as invalid credentials, an unknown brand, or a missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Re:amaze returned an invalid or oversized conversation page.", True),
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
        BranchDefinition(FOUND, "The conversation and its newest messages were read.", False),
        BranchDefinition(NOT_FOUND, "The conversation does not exist in the brand or is not visible to the connection's user.", True),
        BranchDefinition(PROVIDER_REJECTED, "Re:amaze conclusively rejected the conversation read.", True),
        BranchDefinition(INVALID_RESPONSE, "Re:amaze returned an invalid or oversized conversation or message page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_CONVERSATION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_CONVERSATION_OPERATION_ID),
    (
        BranchDefinition(CREATED, "The conversation exists; a retried attempt that found the conversation an earlier attempt created reports it instead of sending again.", False),
        BranchDefinition(PROVIDER_REJECTED, "Re:amaze conclusively rejected the conversation, such as an unknown channel or an invalid status; no conversation was created.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but its outcome cannot be confirmed and no conversation carries the Step's key, so it may still appear; the connector never resends it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
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
        BranchDefinition(NOT_FOUND, "The conversation does not exist in the brand or is not visible to the connection's user.", True),
        BranchDefinition(PROVIDER_REJECTED, "Re:amaze conclusively rejected the change, such as an assignee email that is not a staff user.", True),
        BranchDefinition(INVALID_RESPONSE, "Re:amaze returned an invalid or oversized conversation, so the change may have been applied.", True),
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
        BranchDefinition(REPLIED, "The reply or note is in the conversation; a retried attempt that found its own earlier message reports it instead of sending again.", False),
        BranchDefinition(NOT_FOUND, "The conversation does not exist in the brand or is not visible to the connection's user; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Re:amaze conclusively rejected the message; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "The message was sent but its outcome cannot be confirmed and the conversation shows no message with its origin_id, so it may still appear; the connector never resends it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)

FIND_CONTACT_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_CONTACT_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "At least one contact has the email address.", False),
        BranchDefinition(NOT_FOUND, "No contact on the first page of candidates has the email address; hasMoreCandidates reports whether Re:amaze listed more.", True),
        BranchDefinition(PROVIDER_REJECTED, "Re:amaze conclusively rejected the contact search.", True),
        BranchDefinition(INVALID_RESPONSE, "Re:amaze returned an invalid or oversized contact page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
