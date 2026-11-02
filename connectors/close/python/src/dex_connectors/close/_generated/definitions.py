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

CONNECTOR_ID = "close"
PROVIDER = "close"
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
        BranchDefinition(LISTED, "Close returned the requested data.", False),
        BranchDefinition(PROVIDER_REJECTED, "Close conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Close returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_LEAD_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_LEAD_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Close returned the requested data.", False),
        BranchDefinition(NOT_FOUND, "Close does not have the requested resource.", True),
        BranchDefinition(PROVIDER_REJECTED, "Close conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Close returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
