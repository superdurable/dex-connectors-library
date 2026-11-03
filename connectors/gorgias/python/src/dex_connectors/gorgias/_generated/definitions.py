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

CONNECTOR_ID = "gorgias"
PROVIDER = "gorgias"
SEARCH_TICKETS_OPERATION_ID = "searchTickets"
GET_TICKET_OPERATION_ID = "getTicket"
FIND_CUSTOMER_BY_EMAIL_OPERATION_ID = "findCustomerByEmail"
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
        BranchDefinition(SEARCHED, "One page of matching tickets was returned; it may be empty while a next cursor is set.", False),
        BranchDefinition(PROVIDER_REJECTED, "Gorgias conclusively rejected the list, such as invalid credentials, a missing permission, an unknown view, or an expired cursor.", True),
        BranchDefinition(INVALID_RESPONSE, "Gorgias returned an invalid or oversized customer or ticket page.", True),
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
        BranchDefinition(FOUND, "The ticket and its latest messages were read.", False),
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's user.", True),
        BranchDefinition(PROVIDER_REJECTED, "Gorgias conclusively rejected the ticket read, such as for a merged ticket.", True),
        BranchDefinition(INVALID_RESPONSE, "Gorgias returned an invalid or oversized ticket or message page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

FIND_CUSTOMER_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_CUSTOMER_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "A customer with this primary email address was found.", False),
        BranchDefinition(NOT_FOUND, "No customer has this primary email address.", True),
        BranchDefinition(PROVIDER_REJECTED, "Gorgias conclusively rejected the customer lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Gorgias returned an invalid or oversized customer page.", True),
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
        BranchDefinition(CREATED, "The ticket exists: Gorgias created it now, or a retried attempt found the ticket an earlier attempt of this Step created.", False),
        BranchDefinition(PROVIDER_REJECTED, "Gorgias conclusively rejected the ticket, such as an unknown assignee or an over-long subject; no ticket was created.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but Gorgias did not confirm a ticket for it, so the ticket may still appear; the connector never resends it.", True),
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
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's user.", True),
        BranchDefinition(PROVIDER_REJECTED, "Gorgias conclusively rejected the change, such as an unknown user or team, or a merged ticket; a tag change made before the rejection stays applied, and the Ticket shows it when it can be read again.", True),
        BranchDefinition(INVALID_RESPONSE, "Gorgias returned an invalid or oversized ticket, so the change may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_NOTE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_NOTE_OPERATION_ID),
    (
        BranchDefinition(ADDED, "The note or reply exists: Gorgias added it now, or a retried attempt found the message an earlier attempt of this Step added.", False),
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's user; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Gorgias conclusively rejected the note or reply, or a reply's ticket could not be read, has no customer email to answer, or shows no address known to be the helpdesk's; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but Gorgias did not confirm a message for it, so the note or reply may still appear; the connector never resends it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)
