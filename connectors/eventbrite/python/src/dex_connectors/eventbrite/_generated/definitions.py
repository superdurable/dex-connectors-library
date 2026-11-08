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

CONNECTOR_ID = "eventbrite"
PROVIDER = "eventbrite"
LIST_ORGANIZATIONS_OPERATION_ID = "listOrganizations"
LIST_EVENTS_OPERATION_ID = "listEvents"
GET_ATTENDEE_OPERATION_ID = "getAttendee"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_ORGANIZATIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ORGANIZATIONS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Eventbrite returned the organization page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Eventbrite conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Eventbrite returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_EVENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_EVENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Eventbrite returned the event page.", False),
        BranchDefinition(NOT_FOUND, "The organization does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Eventbrite conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Eventbrite returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_ATTENDEE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ATTENDEE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Eventbrite returned attendee metadata.", False),
        BranchDefinition(NOT_FOUND, "The attendee does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Eventbrite conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Eventbrite returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
