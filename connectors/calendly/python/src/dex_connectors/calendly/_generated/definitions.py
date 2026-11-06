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
        BranchDefinition(INVALID_RESPONSE, "Calendly returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_SCHEDULING_LINK_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_SCHEDULING_LINK_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Calendly created the single-use scheduling link.", False),
        BranchDefinition(PROVIDER_REJECTED, "Calendly conclusively rejected the request, such as an event type the token cannot use.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but Calendly's answer was lost, a server error, or unreadable, so a link may exist that this Result does not name.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=300)),
    ),
)

CREATE_WEBHOOK_SUBSCRIPTION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_WEBHOOK_SUBSCRIPTION_OPERATION_ID),
    (
        BranchDefinition(SUBSCRIBED, "An active subscription with this callback URL covers the requested events; wasCreated tells whether this attempt created it.", False),
        BranchDefinition(CONFLICTING_SUBSCRIPTION, "A subscription with this callback URL already exists but is disabled, lacks a requested event, or belongs to another scope or user; delete it in Calendly and run again.", True),
        BranchDefinition(PROVIDER_REJECTED, "Calendly conclusively rejected the request, such as a plan without webhooks or a token that is not an organization admin.", True),
        BranchDefinition(INVALID_RESPONSE, "Calendly returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=300)),
    ),
)
