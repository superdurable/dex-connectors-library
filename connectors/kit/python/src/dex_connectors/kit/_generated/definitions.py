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

CONNECTOR_ID = "kit"
PROVIDER = "kit"
LIST_SUBSCRIBERS_OPERATION_ID = "listSubscribers"
GET_SUBSCRIBER_OPERATION_ID = "getSubscriber"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_SUBSCRIBERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SUBSCRIBERS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Kit returned the selected subscriber page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Kit conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Kit returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
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
        BranchDefinition(FOUND, "Kit returned subscriber metadata.", False),
        BranchDefinition(NOT_FOUND, "No subscriber with this ID is visible to the API user.", True),
        BranchDefinition(PROVIDER_REJECTED, "Kit conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Kit returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
