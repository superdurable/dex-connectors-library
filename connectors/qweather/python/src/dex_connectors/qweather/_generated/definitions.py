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

CONNECTOR_ID = "qweather"
PROVIDER = "qweather"
GET_CURRENT_WEATHER_OPERATION_ID = "getCurrentWeather"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_CURRENT_WEATHER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CURRENT_WEATHER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "QWeather returned current weather for the coordinates.", False),
        BranchDefinition(PROVIDER_REJECTED, "QWeather rejected authentication, host, or location. HTTP 429 rate and monthly limits retry instead.", True),
        BranchDefinition(INVALID_RESPONSE, "QWeather returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local coordinates, credentials, or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=20), 3, timedelta(seconds=300)),
    ),
)
