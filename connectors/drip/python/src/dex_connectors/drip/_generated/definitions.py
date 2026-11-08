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

CONNECTOR_ID = "drip"
PROVIDER = "drip"
LIST_ACCOUNTS_OPERATION_ID = "listAccounts"
GET_SUBSCRIBER_OPERATION_ID = "getSubscriber"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_ACCOUNTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ACCOUNTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Drip returned accessible accounts.", False),
        BranchDefinition(PROVIDER_REJECTED, "Drip conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Drip returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_SUBSCRIBER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SUBSCRIBER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Drip returned the selected subscriber.", False),
        BranchDefinition(NOT_FOUND, "No active subscriber matches this ID or email in the selected account.", True),
        BranchDefinition(PROVIDER_REJECTED, "Drip conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Drip returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Invalid input or unavailable credentials.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
