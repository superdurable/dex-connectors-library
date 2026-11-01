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

CONNECTOR_ID = "resend"
PROVIDER = "resend"
SEND_EMAIL_OPERATION_ID = "sendEmail"
GET_EMAIL_OPERATION_ID = "getEmail"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SENT = "sent"

SEND_EMAIL_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_EMAIL_OPERATION_ID),
    (
        BranchDefinition(SENT, "Resend returned the requested result.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Resend returned the requested result.", False),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
