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

CONNECTOR_ID = "google-calendar"
PROVIDER = "google"
LIST_EVENTS_OPERATION_ID = "listEvents"
GET_EVENT_OPERATION_ID = "getEvent"
CREATE_EVENT_OPERATION_ID = "createEvent"
UPDATE_EVENT_OPERATION_ID = "updateEvent"
QUERY_FREE_BUSY_OPERATION_ID = "queryFreeBusy"
CONFLICT = "conflict"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INCOMPLETE = "incomplete"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
QUERIED = "queried"
UPDATED = "updated"

LIST_EVENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_EVENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of events in the window was listed.", False),
        BranchDefinition(NOT_FOUND, "The calendar does not exist or is not visible to the connection.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the event listing.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized event page.", True),
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
        BranchDefinition(NOT_FOUND, "The calendar or event does not exist or is not visible to the connection.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the event read.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized event.", True),
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
        BranchDefinition(CREATED, "The event exists under the stable event ID.", False),
        BranchDefinition(CONFLICT, "The stable event ID belongs to a deleted event or to one the connection cannot read.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the event.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized event response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_EVENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_EVENT_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The event holds the requested values.", False),
        BranchDefinition(NOT_FOUND, "The calendar or event does not exist or is not visible to the connection.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the event change.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized event response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

QUERY_FREE_BUSY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_FREE_BUSY_OPERATION_ID),
    (
        BranchDefinition(QUERIED, "Google returned busy intervals for every requested calendar.", False),
        BranchDefinition(INCOMPLETE, "Google reported an error for at least one requested calendar, so its availability is unknown.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the free/busy query.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized free/busy response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
