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

CONNECTOR_ID = "square"
PROVIDER = "square"
LIST_PAYMENTS_OPERATION_ID = "listPayments"
GET_PAYMENT_OPERATION_ID = "getPayment"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_PAYMENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PAYMENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List one page of Square payments.", False),
        BranchDefinition(NOT_FOUND, "The requested resource was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_PAYMENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PAYMENT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Read one Square payment.", False),
        BranchDefinition(NOT_FOUND, "The requested resource was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
