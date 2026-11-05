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

CONNECTOR_ID = "freshdesk"
PROVIDER = "freshdesk"
SEARCH_TICKETS_OPERATION_ID = "searchTickets"
GET_TICKET_OPERATION_ID = "getTicket"
CREATE_TICKET_OPERATION_ID = "createTicket"
UPDATE_TICKET_OPERATION_ID = "updateTicket"
ADD_NOTE_OPERATION_ID = "addNote"
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
        BranchDefinition(PROVIDER_REJECTED, "Freshdesk conclusively rejected the search, such as invalid credentials, a missing permission, or an unknown status.", True),
        BranchDefinition(INVALID_RESPONSE, "Freshdesk returned an invalid or oversized contact list or search page.", True),
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
        BranchDefinition(FOUND, "The ticket and its first conversations were read.", False),
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's agent.", True),
        BranchDefinition(PROVIDER_REJECTED, "Freshdesk conclusively rejected the ticket read.", True),
        BranchDefinition(INVALID_RESPONSE, "Freshdesk returned an invalid or oversized ticket or conversation page.", True),
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
        BranchDefinition(CREATED, "Freshdesk created the ticket and returned it.", False),
        BranchDefinition(PROVIDER_REJECTED, "Freshdesk conclusively rejected the ticket, such as an unknown status, group, or agent; no ticket was created.", True),
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
        BranchDefinition(UPDATED, "The ticket holds the requested values, written now or by an earlier attempt.", False),
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's agent.", True),
        BranchDefinition(PROVIDER_REJECTED, "Freshdesk conclusively rejected the change, such as an unknown status, a missing required field, or a spam or deleted ticket.", True),
        BranchDefinition(INVALID_RESPONSE, "Freshdesk returned an invalid or oversized ticket, so the change may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_NOTE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_NOTE_OPERATION_ID),
    (
        BranchDefinition(ADDED, "Freshdesk added the note or reply and returned it.", False),
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's agent; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Freshdesk conclusively rejected the note or reply; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but its outcome cannot be confirmed, so the note or reply may exist; the connector never resends it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)
