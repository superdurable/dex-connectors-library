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

CONNECTOR_ID = "adyen"
PROVIDER = "adyen"
GET_PAYMENT_LINK_OPERATION_ID = "getPaymentLink"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_PAYMENT_LINK_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PAYMENT_LINK_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Adyen returned the payment link.", False),
        BranchDefinition(NOT_FOUND, "Adyen did not expose the payment link.", True),
        BranchDefinition(PROVIDER_REJECTED, "Adyen conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Adyen returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
