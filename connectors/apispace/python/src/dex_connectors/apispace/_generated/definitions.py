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

CONNECTOR_ID = "apispace"
PROVIDER = "apispace"
GET_CURRENT_WEATHER_OPERATION_ID = "getCurrentWeather"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_CURRENT_WEATHER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CURRENT_WEATHER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "APISpace returned current conditions for the city.", False),
        BranchDefinition(PROVIDER_REJECTED, "APISpace rejected the key, plan, quota, or city.", True),
        BranchDefinition(INVALID_RESPONSE, "APISpace returned malformed or oversized weather data.", True),
        BranchDefinition(DEFECT, "Local connection configuration, token, or city ID is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
