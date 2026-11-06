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

CONNECTOR_ID = "google-sheets"
PROVIDER = "google"
GET_VALUES_OPERATION_ID = "getValues"
FIND_ROW_OPERATION_ID = "findRow"
UPSERT_ROW_OPERATION_ID = "upsertRow"
CONFLICT = "conflict"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
READ = "read"
UNCERTAIN = "uncertain"
UPSERTED = "upserted"

GET_VALUES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_VALUES_OPERATION_ID),
    (
        BranchDefinition(READ, "The range was read.", False),
        BranchDefinition(NOT_FOUND, "The spreadsheet or range does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the range read.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized range response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

FIND_ROW_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_ROW_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Exactly one matching row was found.", False),
        BranchDefinition(NOT_FOUND, "No matching row exists.", True),
        BranchDefinition(CONFLICT, "More than one matching row exists.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the row query.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized row response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPSERT_ROW_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPSERT_ROW_OPERATION_ID),
    (
        BranchDefinition(UPSERTED, "One row was inserted or updated.", False),
        BranchDefinition(CONFLICT, "Existing duplicate key rows prevent a safe write.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the row write.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized pre-write response.", True),
        BranchDefinition(UNCERTAIN, "The dispatched write outcome cannot be confirmed.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
