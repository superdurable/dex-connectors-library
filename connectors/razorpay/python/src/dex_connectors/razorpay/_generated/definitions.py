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

CONNECTOR_ID = "razorpay"
PROVIDER = "razorpay"
GET_PAYMENT_OPERATION_ID = "getPayment"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_PAYMENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PAYMENT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Razorpay returned the payment.", False),
        BranchDefinition(NOT_FOUND, "Razorpay did not expose the payment.", True),
        BranchDefinition(PROVIDER_REJECTED, "Razorpay conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Razorpay returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
