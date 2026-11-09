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

CONNECTOR_ID = "adobe-marketo"
PROVIDER = "adobe"
GET_LEAD_OPERATION_ID = "getLead"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_LEAD_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_LEAD_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Marketo returned the selected lead.", False),
        BranchDefinition(NOT_FOUND, "Marketo returned no lead for the ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Marketo conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Marketo returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
