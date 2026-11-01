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

CONNECTOR_ID = "postmark"
PROVIDER = "postmark"
LIST_OUTBOUND_MESSAGES_OPERATION_ID = "listOutboundMessages"
GET_MESSAGE_DETAILS_OPERATION_ID = "getMessageDetails"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_OUTBOUND_MESSAGES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_OUTBOUND_MESSAGES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Postmark returned the requested result.", False),
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

GET_MESSAGE_DETAILS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_MESSAGE_DETAILS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Postmark returned the requested result.", False),
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
