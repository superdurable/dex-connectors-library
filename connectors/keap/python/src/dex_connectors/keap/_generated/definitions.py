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

CONNECTOR_ID = "keap"
PROVIDER = "keap"
GET_CONTACT_OPERATION_ID = "getContact"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_CONTACT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CONTACT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Keap returned the contact.", False),
        BranchDefinition(NOT_FOUND, "Keap returned 404 for this contact.", True),
        BranchDefinition(PROVIDER_REJECTED, "Keap conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Keap returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
