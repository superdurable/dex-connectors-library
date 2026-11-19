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

CONNECTOR_ID = "lianlianpay"
PROVIDER = "lianlianpay"
QUERY_PAYMENT_STATUS_OPERATION_ID = "queryPaymentStatus"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

QUERY_PAYMENT_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_PAYMENT_STATUS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "LianLian Pay returned the payment status.", False),
        BranchDefinition(PROVIDER_REJECTED, "LianLian Pay rejected the merchant or order query.", True),
        BranchDefinition(INVALID_RESPONSE, "LianLian Pay returned malformed or oversized order data.", True),
        BranchDefinition(DEFECT, "The order number or connection is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
