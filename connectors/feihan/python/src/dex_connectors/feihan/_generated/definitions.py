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

CONNECTOR_ID = "feihan"
PROVIDER = "feihan"
QUERY_RECHARGE_ORDER_OPERATION_ID = "queryRechargeOrder"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

QUERY_RECHARGE_ORDER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_RECHARGE_ORDER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Feihan returned the current recharge status.", False),
        BranchDefinition(PROVIDER_REJECTED, "Feihan rejected the credentials or order identifier.", True),
        BranchDefinition(INVALID_RESPONSE, "Feihan returned malformed or oversized order data.", True),
        BranchDefinition(DEFECT, "The local order identifier or connection is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
