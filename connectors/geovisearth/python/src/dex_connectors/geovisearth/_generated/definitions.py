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

CONNECTOR_ID = "geovisearth"
PROVIDER = "geovisearth"
GET_CURRENT_WEATHER_OPERATION_ID = "getCurrentWeather"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_CURRENT_WEATHER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CURRENT_WEATHER_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "GEOVIS Earth returned current weather.", False),
        BranchDefinition(PROVIDER_REJECTED, "GEOVIS Earth rejected token, quota, or location.", True),
        BranchDefinition(INVALID_RESPONSE, "GEOVIS Earth returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local token or location is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
