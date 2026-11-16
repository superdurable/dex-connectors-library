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

CONNECTOR_ID = "amap"
PROVIDER = "amap"
GEOCODE_ADDRESS_OPERATION_ID = "geocodeAddress"
DEFECT = "defect"
GEOCODED = "geocoded"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GEOCODE_ADDRESS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GEOCODE_ADDRESS_OPERATION_ID),
    (
        BranchDefinition(GEOCODED, "Amap completed the lookup and returned zero or more locations.", False),
        BranchDefinition(PROVIDER_REJECTED, "Amap rejected the key, quota, permissions, or query.", True),
        BranchDefinition(INVALID_RESPONSE, "Amap returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
