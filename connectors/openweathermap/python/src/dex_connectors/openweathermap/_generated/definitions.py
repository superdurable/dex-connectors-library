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

CONNECTOR_ID = "openweathermap"
PROVIDER = "openweathermap"
GET_CURRENT_WEATHER_OPERATION_ID = "getCurrentWeather"
GET_FORECAST_OPERATION_ID = "getForecast"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_CURRENT_WEATHER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CURRENT_WEATHER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "OpenWeatherMap returned the requested result.", False),
        BranchDefinition(NOT_FOUND, "The requested coordinates have no weather data.", True),
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

GET_FORECAST_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_FORECAST_OPERATION_ID),
    (
        BranchDefinition(FOUND, "OpenWeatherMap returned the requested result.", False),
        BranchDefinition(NOT_FOUND, "The requested coordinates have no weather data.", True),
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
