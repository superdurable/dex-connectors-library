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

CONNECTOR_ID = "calendly"
PROVIDER = "calendly"
LIST_SCHEDULED_EVENTS_OPERATION_ID = "listScheduledEvents"
GET_SCHEDULED_EVENT_OPERATION_ID = "getScheduledEvent"
LIST_EVENT_INVITEES_OPERATION_ID = "listEventInvitees"
CANCEL_SCHEDULED_EVENT_OPERATION_ID = "cancelScheduledEvent"
CREATE_SCHEDULING_LINK_OPERATION_ID = "createSchedulingLink"
CREATE_WEBHOOK_SUBSCRIPTION_OPERATION_ID = "createWebhookSubscription"
CANCELED = "canceled"
CONFLICTING_SUBSCRIPTION = "conflictingSubscription"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SUBSCRIBED = "subscribed"
UNCERTAIN = "uncertain"

LIST_SCHEDULED_EVENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SCHEDULED_EVENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Calendly returned one page of scheduled events and the token for the next page, if any.", False),
        BranchDefinition(PROVIDER_REJECTED, "Calendly conclusively rejected the request, such as an invalid token, a missing scope, or an organization the token cannot read.", True),
        BranchDefinition(INVALID_RESPONSE, "Calendly returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_SCHEDULED_EVENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SCHEDULED_EVENT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Calendly returned the scheduled event.", False),
        BranchDefinition(NOT_FOUND, "The scheduled event does not exist or is not visible to the token.", True),
        BranchDefinition(PROVIDER_REJECTED, "Calendly conclusively rejected the request, such as an invalid token or a missing scope.", True),
        BranchDefinition(INVALID_RESPONSE, "Calendly returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_EVENT_INVITEES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_EVENT_INVITEES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Calendly returned one page of invitees and the token for the next page, if any.", False),
        BranchDefinition(NOT_FOUND, "The scheduled event does not exist or is not visible to the token.", True),
        BranchDefinition(PROVIDER_REJECTED, "Calendly conclusively rejected the request, such as an invalid token or a missing scope.", True),
        BranchDefinition(INVALID_RESPONSE, "Calendly returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CANCEL_SCHEDULED_EVENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CANCEL_SCHEDULED_EVENT_OPERATION_ID),
    (
        BranchDefinition(CANCELED, "The scheduled event is canceled, by this call or earlier; alreadyCanceled tells which.", False),
        BranchDefinition(NOT_FOUND, "The scheduled event does not exist or is not visible to the token.", True),
        BranchDefinition(PROVIDER_REJECTED, "Calendly refused to cancel an active event, such as one that already started or one the token may not cancel.", True),
        BranchDefinition(INVALID_RESPONSE, "Calendly returned a malformed or oversized scheduled event when the connector read it back after a 403.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.SAFE,
)

CREATE_SCHEDULING_LINK_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_SCHEDULING_LINK_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Calendly created the single-use scheduling link.", False),
        BranchDefinition(PROVIDER_REJECTED, "Calendly conclusively rejected the first send with HTTP 400, 401, 403, 404, 411, 413, 414, 415, or 422, such as an event type the token cannot use.", True),
        BranchDefinition(UNCERTAIN, "Sends up to the send cap had a lost, server-error, conflicting, or unreadable answer, or Calendly accepted the link without a usable URL, so a link may exist that this Result does not name.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=300)),
    ),
    repeat_on_unknown=RepeatOnUnknown.LIMITED,
    max_sends=3,
)

CREATE_WEBHOOK_SUBSCRIPTION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_WEBHOOK_SUBSCRIPTION_OPERATION_ID),
    (
        BranchDefinition(SUBSCRIBED, "An active subscription with this callback URL covers the requested events; wasCreated tells whether this attempt created it.", False),
        BranchDefinition(CONFLICTING_SUBSCRIPTION, "A subscription with this callback URL already exists but is disabled, lacks a requested event, or belongs to another scope or user; delete it in Calendly and run again.", True),
        BranchDefinition(PROVIDER_REJECTED, "Calendly conclusively rejected the request, such as a plan without webhooks or a token that is not an organization admin.", True),
        BranchDefinition(INVALID_RESPONSE, "Calendly returned a malformed or oversized user or subscription list before the connector created anything.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=300)),
    ),
    repeat_on_unknown=RepeatOnUnknown.SAFE,
)

INVITEE_EVENT_RECEIVED_TRIGGER_DEFINITION = TriggerDefinition(
    trigger=TriggerRef(connector_id=CONNECTOR_ID, trigger_name="inviteeEventReceived"),
    description="Receive a signed Calendly invitee.created or invitee.canceled webhook, with the invitee's stable URI as its event ID.",
)
