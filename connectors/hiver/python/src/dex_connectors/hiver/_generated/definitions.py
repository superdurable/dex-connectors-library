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

CONNECTOR_ID = "hiver"
PROVIDER = "hiver"
LIST_INBOXES_OPERATION_ID = "listInboxes"
LIST_CONVERSATIONS_OPERATION_ID = "listConversations"
GET_CONVERSATION_OPERATION_ID = "getConversation"
UPDATE_CONVERSATION_OPERATION_ID = "updateConversation"
ADD_NOTE_OPERATION_ID = "addNote"
CREATE_SHARED_DRAFT_OPERATION_ID = "createSharedDraft"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"
UPDATED = "updated"

LIST_INBOXES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_INBOXES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Hiver returned one page of shared inboxes; it may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Hiver conclusively rejected the request, such as an invalid or disabled API key or an account without API access.", True),
        BranchDefinition(INVALID_RESPONSE, "Hiver returned a malformed or oversized page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_CONVERSATIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CONVERSATIONS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Hiver returned one page of the inbox's conversations; it may be empty.", False),
        BranchDefinition(NOT_FOUND, "The shared inbox does not exist or is not visible to the API key.", True),
        BranchDefinition(PROVIDER_REJECTED, "Hiver conclusively rejected the request, such as an invalid API key or an expired page token.", True),
        BranchDefinition(INVALID_RESPONSE, "Hiver returned a malformed or oversized page.", True),
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
        BranchDefinition(FOUND, "Hiver returned the conversation.", False),
        BranchDefinition(NOT_FOUND, "The conversation or shared inbox does not exist or is not visible to the API key.", True),
        BranchDefinition(PROVIDER_REJECTED, "Hiver conclusively rejected the read, such as an invalid API key.", True),
        BranchDefinition(INVALID_RESPONSE, "Hiver returned a malformed or oversized conversation.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_CONVERSATION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_CONVERSATION_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The conversation, read back after the change, holds the requested status, assignee, and tags.", False),
        BranchDefinition(NOT_FOUND, "The conversation or shared inbox does not exist or is not visible to the API key.", True),
        BranchDefinition(PROVIDER_REJECTED, "Hiver conclusively rejected the change, or the assignee email or a tag name was not found in the shared inbox; this attempt changed nothing, though an earlier attempt that ended unconfirmed may have applied the change.", True),
        BranchDefinition(INVALID_RESPONSE, "Hiver returned a malformed or oversized user, tag, or conversation response, or rejected the read-back after accepting the change, so part or all of the change may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=60),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_NOTE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_NOTE_OPERATION_ID),
    (
        BranchDefinition(ADDED, "Hiver added the note and returned it.", False),
        BranchDefinition(NOT_FOUND, "The conversation or shared inbox does not exist or is not visible to the API key; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Hiver conclusively rejected the note; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "The note may exist: Hiver's answer was lost, unreadable, 408, 409, another server error besides 503, or an earlier attempt may have sent it. The Step does not send it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)

CREATE_SHARED_DRAFT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_SHARED_DRAFT_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Hiver created the shared draft and returned its ID.", False),
        BranchDefinition(NOT_FOUND, "The message or shared inbox does not exist or is not visible to the API key; no draft was created.", True),
        BranchDefinition(PROVIDER_REJECTED, "Hiver conclusively rejected the draft; no draft was created.", True),
        BranchDefinition(UNCERTAIN, "The draft may exist: Hiver's answer was lost, unreadable, 408, 409, another server error besides 503, or an earlier attempt may have sent it. The Step does not send it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)
