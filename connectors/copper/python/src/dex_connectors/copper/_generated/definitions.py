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

CONNECTOR_ID = "copper"
PROVIDER = "copper"
LIST_LEAD_STATUSES_OPERATION_ID = "listLeadStatuses"
GET_LEAD_OPERATION_ID = "getLead"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_LEAD_STATUSES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_LEAD_STATUSES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Copper returned lead statuses.", False),
        BranchDefinition(PROVIDER_REJECTED, "Copper conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Copper returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Connection credentials are invalid.", True),
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
        BranchDefinition(FOUND, "Copper returned the selected lead.", False),
        BranchDefinition(NOT_FOUND, "No lead matches this ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Copper conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Copper returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Invalid input or unavailable credentials.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
