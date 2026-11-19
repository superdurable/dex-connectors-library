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

CONNECTOR_ID = "baidu-maps"
PROVIDER = "baidu-maps"
GEOCODE_ADDRESS_OPERATION_ID = "geocodeAddress"
DEFECT = "defect"
GEOCODED = "geocoded"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GEOCODE_ADDRESS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GEOCODE_ADDRESS_OPERATION_ID),
    (
        BranchDefinition(GEOCODED, "Baidu Maps returned one geocoded coordinate and precision metrics.", False),
        BranchDefinition(PROVIDER_REJECTED, "Baidu Maps rejected the AK, permissions, quota, or address.", True),
        BranchDefinition(INVALID_RESPONSE, "Baidu Maps returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
