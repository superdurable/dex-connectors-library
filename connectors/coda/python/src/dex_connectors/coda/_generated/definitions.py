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

CONNECTOR_ID = "coda"
PROVIDER = "coda"
LIST_ROWS_OPERATION_ID = "listRows"
GET_ROW_OPERATION_ID = "getRow"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_ROWS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ROWS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List one page of rows in a Coda table.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input, configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_ROW_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ROW_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Get one row in a Coda table.", False),
        BranchDefinition(NOT_FOUND, "The requested provider resource does not exist or is not visible.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input, configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
