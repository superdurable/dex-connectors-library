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
    TriggerDefinition,
    TriggerRef,
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
        BranchDefinition(UNCERTAIN, "The reply may exist but could not be confirmed: the send cap is reached and the conversation shows no part by the admin with the reply's type and text; or, after a possible send, the conversation could not be read back, is missing, or shows the reply with an invalid part; or Intercom accepted the reply with an unusable answer that a read-back could not confirm. A refusal whose body is oversized or reflects the access token is unconfirmed like a lost answer. Do not send it again without checking the conversation. Below the cap, a read-back that shows no matching part resends.", True),
        BranchDefinition(INVALID_RESPONSE, "No longer selected. A reply whose part is invalid may exist, so the connector selects uncertain instead; the branch stays declared for compatibility.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
    repeat_on_unknown=RepeatOnUnknown.LIMITED,
    max_sends=3,
)

UPDATE_CONVERSATION_STATE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_CONVERSATION_STATE_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The conversation is in the requested state; a repeated attempt that finds it there writes nothing.", False),
        BranchDefinition(NOT_FOUND, "The conversation does not exist or is not visible to the connection's token.", True),
        BranchDefinition(PROVIDER_REJECTED, "Intercom conclusively rejected the change, such as an unknown admin, and the conversation is not in the requested state.", True),
        BranchDefinition(INVALID_RESPONSE, "Kept for compatibility and no longer selected: an invalid, oversized, or credential-reflecting conversation read before the change returns Retry, because the change is safe to repeat.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.SAFE,
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

CONVERSATION_EVENT_TRIGGER_DEFINITION = TriggerDefinition(
    trigger=TriggerRef(connector_id=CONNECTOR_ID, trigger_name="conversationEvent"),
    description="Receive one Intercom conversation webhook notification, verified with its X-Hub-Signature, such as a new inbound conversation or a customer reply, filtered by topic.",
)
