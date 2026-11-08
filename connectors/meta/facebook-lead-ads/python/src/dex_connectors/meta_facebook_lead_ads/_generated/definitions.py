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

CONNECTOR_ID = "facebook-lead-ads"
PROVIDER = "facebook-lead-ads"
LIST_LEAD_FORMS_OPERATION_ID = "listLeadForms"
GET_LEAD_OPERATION_ID = "getLead"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_LEAD_FORMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_LEAD_FORMS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Meta returned one lead-form page.", False),
        BranchDefinition(NOT_FOUND, "The Page is missing or inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Meta definitively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Meta returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
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
        BranchDefinition(FOUND, "Meta returned lead identity metadata.", False),
        BranchDefinition(NOT_FOUND, "The lead is missing or inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Meta definitively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Meta returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
