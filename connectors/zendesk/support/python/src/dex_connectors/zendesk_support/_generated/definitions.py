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

CONNECTOR_ID = "zendesk-support"
PROVIDER = "zendesk"
SEARCH_TICKETS_OPERATION_ID = "searchTickets"
GET_TICKET_OPERATION_ID = "getTicket"
CREATE_TICKET_OPERATION_ID = "createTicket"
UPDATE_TICKET_OPERATION_ID = "updateTicket"
CONFLICT = "conflict"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"
UPDATED = "updated"

SEARCH_TICKETS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_TICKETS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of matching tickets was returned; it may be empty.", False),
        BranchDefinition(
            PROVIDER_REJECTED,
            "Zendesk conclusively rejected the search, such as invalid credentials, a missing permission, or an expired cursor.",
            True,
        ),
        BranchDefinition(INVALID_RESPONSE, "Zendesk returned an invalid or oversized search page.", True),
        BranchDefinition(
            DEFECT, "Local input, connection configuration, or connector definition is invalid.", True
        ),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_TICKET_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TICKET_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The ticket and its latest comments were read.", False),
        BranchDefinition(
            NOT_FOUND, "The ticket does not exist or is not visible to the connection's agent.", True
        ),
        BranchDefinition(PROVIDER_REJECTED, "Zendesk conclusively rejected the ticket read.", True),
        BranchDefinition(
            INVALID_RESPONSE, "Zendesk returned an invalid or oversized ticket or comment page.", True
        ),
        BranchDefinition(
            DEFECT, "Local input, connection configuration, or connector definition is invalid.", True
        ),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_TICKET_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_TICKET_OPERATION_ID),
    (
        BranchDefinition(
            CREATED,
            "The ticket exists; a repeated attempt returns the ticket the first attempt created.",
            False,
        ),
        BranchDefinition(
            PROVIDER_REJECTED,
            "Zendesk conclusively rejected the ticket, such as an invalid requester, group, or assignee; no ticket was created.",
            True,
        ),
        BranchDefinition(
            INVALID_RESPONSE,
            "Zendesk accepted the request but returned an invalid or oversized ticket, so the ticket may exist.",
            True,
        ),
        BranchDefinition(
            DEFECT, "Local input, connection configuration, or connector definition is invalid.", True
        ),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_TICKET_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_TICKET_OPERATION_ID),
    (
        BranchDefinition(
            UPDATED,
            "The ticket holds the requested change; a repeated attempt finds its own earlier change instead of adding a second comment.",
            False,
        ),
        BranchDefinition(
            NOT_FOUND, "The ticket does not exist or is not visible to the connection's agent.", True
        ),
        BranchDefinition(
            CONFLICT, "The ticket changed after expectedUpdatedAt, so nothing was written.", True
        ),
        BranchDefinition(
            PROVIDER_REJECTED,
            "Zendesk conclusively rejected the change, such as updating a closed ticket or assigning an unknown agent.",
            True,
        ),
        BranchDefinition(
            INVALID_RESPONSE,
            "Zendesk returned an invalid or oversized ticket or audit response, so the change may have been applied.",
            True,
        ),
        BranchDefinition(
            DEFECT, "Local input, connection configuration, or connector definition is invalid.", True
        ),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=300)),
    ),
)
