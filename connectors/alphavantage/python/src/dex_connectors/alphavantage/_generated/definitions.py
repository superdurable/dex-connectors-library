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

CONNECTOR_ID = "alphavantage"
PROVIDER = "alphavantage"
GET_GLOBAL_QUOTE_OPERATION_ID = "getGlobalQuote"
GET_TIME_SERIES_DAILY_OPERATION_ID = "getTimeSeriesDaily"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_GLOBAL_QUOTE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_GLOBAL_QUOTE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Alpha Vantage returned the requested result.", False),
        BranchDefinition(NOT_FOUND, "No market data exists for the requested symbol.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_TIME_SERIES_DAILY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TIME_SERIES_DAILY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Alpha Vantage returned the requested result.", False),
        BranchDefinition(NOT_FOUND, "No market data exists for the requested symbol.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
