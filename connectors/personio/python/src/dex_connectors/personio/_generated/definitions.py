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

CONNECTOR_ID = "personio"
PROVIDER = "personio"
GET_PERSON_OPERATION_ID = "getPerson"
LIST_PERSONS_OPERATION_ID = "listPersons"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_PERSON_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PERSON_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Personio returned the selected person.", False),
        BranchDefinition(NOT_FOUND, "Personio returned no accessible person for this ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Personio conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Personio returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_PERSONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PERSONS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Personio returned one page of selected persons.", False),
        BranchDefinition(PROVIDER_REJECTED, "Personio conclusively rejected the list request.", True),
        BranchDefinition(INVALID_RESPONSE, "Personio returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local filters or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
