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

CONNECTOR_ID = "microsoft-dynamics-365-sales"
PROVIDER = "microsoft-dynamics-365-sales"
FIND_CONTACT_OPERATION_ID = "findContact"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MISSING = "missing"
MULTIPLE_MATCHES = "multipleMatches"
PROVIDER_REJECTED = "providerRejected"

FIND_CONTACT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_CONTACT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Exactly one contact matched the email.", False),
        BranchDefinition(MISSING, "No contact matched the email.", True),
        BranchDefinition(MULTIPLE_MATCHES, "More than one contact matched the email.", True),
        BranchDefinition(PROVIDER_REJECTED, "Entra or Dataverse conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Entra or Dataverse returned invalid or oversized JSON.", True),
        BranchDefinition(DEFECT, "The configuration, credentials, or email is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
