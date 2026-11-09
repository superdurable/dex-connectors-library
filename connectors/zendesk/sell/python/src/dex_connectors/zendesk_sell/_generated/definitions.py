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

CONNECTOR_ID = "zendesk-sell"
PROVIDER = "zendesk-sell"
LIST_LEADS_OPERATION_ID = "listLeads"
GET_LEAD_OPERATION_ID = "getLead"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_LEADS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_LEADS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Zendesk Sell returned the selected lead page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zendesk Sell conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Zendesk Sell returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Invalid page input or unavailable credentials.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_LEAD_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_LEAD_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Zendesk Sell returned the selected lead.", False),
        BranchDefinition(NOT_FOUND, "No lead matches this ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zendesk Sell conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Zendesk Sell returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Invalid lead ID or unavailable credentials.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
