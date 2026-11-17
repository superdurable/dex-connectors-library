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
UNCERTAIN = "uncertain"
UPDATED = "updated"

SEARCH_TICKETS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_TICKETS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of matching tickets was returned; it may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zendesk conclusively rejected the search, such as invalid credentials, a missing permission, or an expired cursor.", True),
        BranchDefinition(INVALID_RESPONSE, "Zendesk returned an invalid or oversized search page.", True),
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
        BranchDefinition(FOUND, "The ticket and its latest comments were read.", False),
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's agent.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zendesk conclusively rejected the ticket read.", True),
        BranchDefinition(INVALID_RESPONSE, "Zendesk returned an invalid or oversized ticket or comment page.", True),
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
        BranchDefinition(CREATED, "The ticket exists; a repeated attempt returns the ticket the first attempt created.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zendesk conclusively rejected the ticket with HTTP 400, 401, 403, 404, 411, 413, 414, 415, or 422, such as an invalid requester, group, or assignee, or a reused Idempotency-Key with another body; no ticket was created.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.SAFE,
)

UPDATE_TICKET_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_TICKET_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The ticket holds the requested change; a repeated attempt finds its own earlier change instead of adding a second comment.", False),
        BranchDefinition(NOT_FOUND, "The ticket does not exist or is not visible to the connection's agent, and no earlier attempt of this Step may have written it.", True),
        BranchDefinition(CONFLICT, "The ticket read before the first write changed after expectedUpdatedAt, so nothing was written.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zendesk conclusively rejected the reads or the first write with HTTP 400, 401, 403, 411, 413, 414, 415, or 422, such as updating a closed ticket or assigning an unknown agent; nothing was written.", True),
        BranchDefinition(INVALID_RESPONSE, "Zendesk returned an invalid or oversized ticket or audit page on the read before the first write, so nothing was written.", True),
        BranchDefinition(UNCERTAIN, "The send cap is reached, or Zendesk refused or contradicted the request after an earlier send whose outcome could not be confirmed, and the latest audit page shows no change by this Step, so the change and its comment may still appear. Below the cap, an unconfirmed write is read back and then resent.", True),
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

TICKET_CHANGED_TRIGGER_DEFINITION = TriggerDefinition(
    trigger=TriggerRef(connector_id=CONNECTOR_ID, trigger_name="ticketChanged"),
    description="Receive each ticket created, updated, or deleted in the account, read through Zendesk's cursor-based incremental ticket export; the first run records a baseline and delivers no existing tickets. Requires an admin connection.",
)
