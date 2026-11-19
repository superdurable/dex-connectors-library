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

CONNECTOR_ID = "outlook-calendar"
PROVIDER = "microsoft"
LIST_EVENTS_OPERATION_ID = "listEvents"
GET_EVENT_OPERATION_ID = "getEvent"
CREATE_EVENT_OPERATION_ID = "createEvent"
UPDATE_EVENT_OPERATION_ID = "updateEvent"
QUERY_FREE_BUSY_OPERATION_ID = "queryFreeBusy"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INCOMPLETE = "incomplete"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
QUERIED = "queried"
UNCERTAIN = "uncertain"
UPDATED = "updated"

LIST_EVENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_EVENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of events in the window was listed.", False),
        BranchDefinition(NOT_FOUND, "The calendar does not exist or is not visible to the connection.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the calendar view.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized event page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_EVENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_EVENT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The event was read.", False),
        BranchDefinition(NOT_FOUND, "The event does not exist or is not visible to the connection.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the event read.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized event.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_EVENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_EVENT_OPERATION_ID),
    (
        BranchDefinition(CREATED, "The event that carries the Step's idempotency key exists.", False),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the event.", True),
        BranchDefinition(INVALID_RESPONSE, "The marker lookup before any send returned a malformed or oversized answer, so nothing was sent; an unusable POST answer is read back or selects uncertain instead.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
        BranchDefinition(UNCERTAIN, "An earlier send may have created the event, the marker lookup cannot confirm it, and the send cap allows no further POST.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
    repeat_on_unknown=RepeatOnUnknown.LIMITED,
    max_sends=3,
)

UPDATE_EVENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_EVENT_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The event holds the requested values.", False),
        BranchDefinition(NOT_FOUND, "The event does not exist, is cancelled, or is not visible to the connection.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the event change.", True),
        BranchDefinition(INVALID_RESPONSE, "Retained for compatibility and no longer selected: an invalid or oversized response now retries from the read.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.SAFE,
)

QUERY_FREE_BUSY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_FREE_BUSY_OPERATION_ID),
    (
        BranchDefinition(QUERIED, "Microsoft Graph reported known availability for every requested schedule.", False),
        BranchDefinition(INCOMPLETE, "Microsoft Graph reported an error, an unknown status, or no data for at least one schedule, so its availability is unknown.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the schedule query.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized schedule response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
