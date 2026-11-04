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

CONNECTOR_ID = "paypal"
PROVIDER = "paypal"
LIST_TRANSACTIONS_OPERATION_ID = "listTransactions"
GET_ORDER_OPERATION_ID = "getOrder"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_TRANSACTIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TRANSACTIONS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List one page of PayPal transactions in a date range.", False),
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

GET_ORDER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ORDER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Read one PayPal checkout order.", False),
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
