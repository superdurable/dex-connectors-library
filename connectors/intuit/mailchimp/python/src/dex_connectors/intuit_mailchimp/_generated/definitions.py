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

CONNECTOR_ID = "mailchimp"
PROVIDER = "mailchimp"
GET_MEMBER_OPERATION_ID = "getMember"
LIST_MEMBERS_OPERATION_ID = "listMembers"
UPSERT_MEMBER_OPERATION_ID = "upsertMember"
UPDATE_MEMBER_TAGS_OPERATION_ID = "updateMemberTags"
SEND_CAMPAIGN_OPERATION_ID = "sendCampaign"
ALREADY_SENT = "alreadySent"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
NOT_SENDABLE = "notSendable"
PROVIDER_REJECTED = "providerRejected"
SENT = "sent"
UNCERTAIN = "uncertain"
UPDATED = "updated"
UPSERTED = "upserted"

GET_MEMBER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_MEMBER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The contact exists in the audience, in any status including unsubscribed, cleaned, or archived.", False),
        BranchDefinition(NOT_FOUND, "The audience has no contact with that address, or the audience does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Mailchimp conclusively rejected the read, such as an invalid or revoked API key or a role without access.", True),
        BranchDefinition(INVALID_RESPONSE, "Mailchimp returned an invalid or oversized contact.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_MEMBERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MEMBERS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of the audience's contacts was read; it may be empty.", False),
        BranchDefinition(NOT_FOUND, "The audience does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Mailchimp conclusively rejected the list, such as an invalid API key or a role without access.", True),
        BranchDefinition(INVALID_RESPONSE, "Mailchimp returned an invalid or oversized page of contacts.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPSERT_MEMBER_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPSERT_MEMBER_OPERATION_ID),
    (
        BranchDefinition(UPSERTED, "Mailchimp holds the contact with the requested values, written now or by an earlier attempt, and returned it.", False),
        BranchDefinition(PROVIDER_REJECTED, "Mailchimp conclusively rejected the contact, such as an invalid merge field, a permanently deleted address, or a contact in a compliance state; nothing was written.", True),
        BranchDefinition(INVALID_RESPONSE, "Mailchimp returned an invalid or oversized contact, so the change may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_MEMBER_TAGS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_MEMBER_TAGS_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Mailchimp accepted the tag changes, including tags the contact already had or already lacked.", False),
        BranchDefinition(NOT_FOUND, "The audience has no contact with that address, or the audience does not exist; nothing was changed.", True),
        BranchDefinition(PROVIDER_REJECTED, "Mailchimp conclusively rejected the tag changes, such as an invalid API key or a role without access.", True),
        BranchDefinition(INVALID_RESPONSE, "Mailchimp accepted the request but returned an oversized or credential-reflecting body, so the tags may have changed.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SEND_CAMPAIGN_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_CAMPAIGN_OPERATION_ID),
    (
        BranchDefinition(SENT, "Mailchimp accepted the send of the draft campaign this Step read.", False),
        BranchDefinition(ALREADY_SENT, "Mailchimp reports the campaign as sending or sent, so this attempt sent nothing.", True),
        BranchDefinition(NOT_SENDABLE, "The campaign is not a draft that can be sent now, such as a scheduled, paused, canceled, or RSS campaign, or it targets another audience than expectedListId; nothing was sent.", True),
        BranchDefinition(NOT_FOUND, "The campaign does not exist; nothing was sent.", True),
        BranchDefinition(PROVIDER_REJECTED, "Mailchimp conclusively rejected the send, such as a campaign that fails its send checklist or a role that cannot send; nothing was sent.", True),
        BranchDefinition(INVALID_RESPONSE, "Mailchimp returned an invalid or oversized campaign before the send; nothing was sent.", True),
        BranchDefinition(UNCERTAIN, "The send request was sent but its outcome cannot be confirmed, so the campaign may be sending; the connector never sends it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=60),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
