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

CONNECTOR_ID = "jira-service-management"
PROVIDER = "atlassian"
FIND_CUSTOMER_BY_EMAIL_OPERATION_ID = "findCustomerByEmail"
SEARCH_TICKETS_OPERATION_ID = "searchTickets"
GET_TICKET_OPERATION_ID = "getTicket"
CREATE_TICKET_OPERATION_ID = "createTicket"
UPDATE_TICKET_OPERATION_ID = "updateTicket"
TRANSITION_TICKET_OPERATION_ID = "transitionTicket"
ADD_COMMENT_OPERATION_ID = "addComment"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"
TRANSITION_UNAVAILABLE = "transitionUnavailable"
TRANSITIONED = "transitioned"
UNCERTAIN = "uncertain"
UPDATED = "updated"

FIND_CUSTOMER_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_CUSTOMER_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "At least one customer of the service desk has exactly this email address.", False),
        BranchDefinition(NOT_FOUND, "No customer of the service desk has this email address, or Jira hides the customer's email address.", True),
        BranchDefinition(PROVIDER_REJECTED, "Jira Service Management conclusively rejected the lookup, such as an unknown service desk or an account that cannot view its customers.", True),
        BranchDefinition(INVALID_RESPONSE, "Jira Service Management returned an invalid or oversized customer page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SEARCH_TICKETS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_TICKETS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of matching tickets was read; Jira search is eventually consistent, so a just-created request can be missing.", False),
        BranchDefinition(PROVIDER_REJECTED, "Jira conclusively rejected the search, such as an unknown project or status, an expired page token, or missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Jira returned an invalid or oversized search page.", True),
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
        BranchDefinition(FOUND, "The request, its first comments, and its SLAs were read.", False),
        BranchDefinition(NOT_FOUND, "The issue does not exist, is not a Jira Service Management request, or the connection cannot browse it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Jira Service Management conclusively rejected a read, such as an SLA read by an account that is not an agent of the desk.", True),
        BranchDefinition(INVALID_RESPONSE, "Jira returned an invalid or oversized request, comment page, or SLA page.", True),
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
        BranchDefinition(CREATED, "Jira Service Management created the request and returned its key.", False),
        BranchDefinition(PROVIDER_REJECTED, "Jira Service Management conclusively rejected the request, such as a missing required field, an unknown request type or customer, or missing permission; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but its outcome cannot be confirmed, so the customer request may exist; the connector never resends it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

UPDATE_TICKET_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_TICKET_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The request holds the requested labels and priority, written now or by an earlier attempt.", False),
        BranchDefinition(NOT_FOUND, "The issue does not exist or the connection cannot browse it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Jira conclusively rejected the change, such as an unknown priority, a field missing from the edit screen, or missing Edit Issues permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Jira returned an invalid or oversized issue.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

TRANSITION_TICKET_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, TRANSITION_TICKET_OPERATION_ID),
    (
        BranchDefinition(TRANSITIONED, "The request is in the transition's destination status, including when it already was.", False),
        BranchDefinition(NOT_FOUND, "The issue does not exist or the connection cannot browse it.", True),
        BranchDefinition(TRANSITION_UNAVAILABLE, "No available transition from the request's current status matches, or several do; the Value lists the current status and the available transitions.", True),
        BranchDefinition(PROVIDER_REJECTED, "Jira conclusively rejected the transition, such as a required transition screen field or missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Jira returned an invalid or oversized issue or transition list.", True),
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
        BranchDefinition(ADDED, "Jira Service Management added the comment and returned its ID.", False),
        BranchDefinition(NOT_FOUND, "The request does not exist or the connection cannot view it; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Jira Service Management conclusively rejected the comment, such as missing Add Comments permission; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "The comment was sent but its outcome cannot be confirmed, so it may exist; the connector never resends it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
