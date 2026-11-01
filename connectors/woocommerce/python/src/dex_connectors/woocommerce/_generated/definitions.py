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

CONNECTOR_ID = "woocommerce"
PROVIDER = "woocommerce"
LIST_ORDERS_OPERATION_ID = "listOrders"
GET_ORDER_OPERATION_ID = "getOrder"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_ORDERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ORDERS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List one page of WooCommerce orders.", False),
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
        BranchDefinition(FOUND, "Read one WooCommerce order.", False),
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
