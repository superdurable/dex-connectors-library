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

CONNECTOR_ID = "jotform"
PROVIDER = "jotform"
LIST_FORMS_OPERATION_ID = "listForms"
GET_SUBMISSION_OPERATION_ID = "getSubmission"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_FORMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_FORMS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Jotform returned the selected form page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Jotform conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Jotform returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_SUBMISSION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SUBMISSION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Jotform returned the submission metadata.", False),
        BranchDefinition(NOT_FOUND, "The submission does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Jotform conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Jotform returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
