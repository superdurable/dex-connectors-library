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

CONNECTOR_ID = "tushare"
PROVIDER = "tushare"
GET_TRADING_DAY_OPERATION_ID = "getTradingDay"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NO_DATA = "noData"
PROVIDER_REJECTED = "providerRejected"

GET_TRADING_DAY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TRADING_DAY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Tushare returned the requested trading date.", False),
        BranchDefinition(NO_DATA, "Tushare returned no matching calendar date.", True),
        BranchDefinition(PROVIDER_REJECTED, "Tushare rejected the token, permission, or query.", True),
        BranchDefinition(INVALID_RESPONSE, "Tushare returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local token, exchange, or date is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
