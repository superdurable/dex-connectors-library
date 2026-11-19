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

CONNECTOR_ID = "caiyun"
PROVIDER = "caiyun"
GET_CURRENT_WEATHER_OPERATION_ID = "getCurrentWeather"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_CURRENT_WEATHER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CURRENT_WEATHER_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "Caiyun returned current weather for the coordinate.", False),
        BranchDefinition(PROVIDER_REJECTED, "Caiyun rejected credentials, permission, or coordinate. HTTP 429 rate or quota limits retry instead.", True),
        BranchDefinition(INVALID_RESPONSE, "Caiyun returned malformed or oversized weather data.", True),
        BranchDefinition(DEFECT, "Local coordinate or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=10), 2, timedelta(seconds=60)),
    ),
)
