# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation identities, branches, and Step defaults."""

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

CONNECTOR_ID = "smartsheet"
PROVIDER = "smartsheet"
LIST_SHEETS_OPERATION_ID = "listSheets"
GET_SHEET_OPERATION_ID = "getSheet"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_SHEETS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SHEETS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Smartsheet returned the requested data.", False),
        BranchDefinition(PROVIDER_REJECTED, "Smartsheet conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Smartsheet returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_SHEET_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SHEET_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Smartsheet returned the requested data.", False),
        BranchDefinition(NOT_FOUND, "Smartsheet does not have the requested resource.", True),
        BranchDefinition(PROVIDER_REJECTED, "Smartsheet conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Smartsheet returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
