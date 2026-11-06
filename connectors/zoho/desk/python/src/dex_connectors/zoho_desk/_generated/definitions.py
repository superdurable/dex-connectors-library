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

CONNECTOR_ID = "zoho-desk"
PROVIDER = "zoho"
SEARCH_TICKETS_OPERATION_ID = "searchTickets"
GET_TICKET_OPERATION_ID = "getTicket"
CREATE_TICKET_OPERATION_ID = "createTicket"
UPDATE_TICKET_OPERATION_ID = "updateTicket"
ADD_COMMENT_OPERATION_ID = "addComment"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"
UNCERTAIN = "uncertain"
UPDATED = "updated"

SEARCH_TICKETS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_TICKETS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of matching tickets was returned; it may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zoho Desk conclusively rejected the search, such as a revoked authorization, a missing scope, another organization, or an invalid filter value.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoho Desk returned an invalid or oversized search page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
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
        BranchDefinition(FOUND, "The ticket, its contact, and its newest threads and comments were read.", False),
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's agent.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zoho Desk conclusively rejected the ticket read.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoho Desk returned an invalid or oversized ticket, thread list, or comment list.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
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
        BranchDefinition(CREATED, "Zoho Desk created the ticket and returned it.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zoho Desk conclusively rejected the ticket, such as an unknown department, status, or assignee; no ticket was created.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but its outcome cannot be confirmed, so the ticket may exist; the connector never resends it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)

UPDATE_TICKET_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_TICKET_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Zoho Desk accepted the requested values now or already held them; the returned ticket shows any change its automations made.", False),
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's agent.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zoho Desk conclusively rejected the change, such as an unknown status, an assignee outside the department, or a blueprint that forbids the transition.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoho Desk returned an invalid or oversized ticket, so the change may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_COMMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_COMMENT_OPERATION_ID),
    (
        BranchDefinition(ADDED, "Zoho Desk added the comment and returned it.", False),
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's agent; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zoho Desk conclusively rejected the comment; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but its outcome cannot be confirmed, so the comment may exist; the connector never resends it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)
