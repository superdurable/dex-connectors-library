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

CONNECTOR_ID = "brevo"
PROVIDER = "brevo"
CREATE_OR_UPDATE_CONTACT_OPERATION_ID = "createOrUpdateContact"
GET_CONTACT_OPERATION_ID = "getContact"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SAVED = "saved"

CREATE_OR_UPDATE_CONTACT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_OR_UPDATE_CONTACT_OPERATION_ID),
    (
        BranchDefinition(SAVED, "Brevo created or updated the contact.", False),
        BranchDefinition(PROVIDER_REJECTED, "Brevo rejected the contact or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Brevo returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_CONTACT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CONTACT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Brevo returned the contact.", False),
        BranchDefinition(NOT_FOUND, "No contact has that email.", True),
        BranchDefinition(PROVIDER_REJECTED, "Brevo rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Brevo returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
