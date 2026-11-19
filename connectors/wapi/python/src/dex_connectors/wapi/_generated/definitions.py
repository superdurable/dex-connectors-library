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

CONNECTOR_ID = "wapi"
PROVIDER = "wapi"
LIST_VEHICLE_BRANDS_OPERATION_ID = "listVehicleBrands"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_VEHICLE_BRANDS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_VEHICLE_BRANDS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Wapi returned the vehicle brand list.", False),
        BranchDefinition(PROVIDER_REJECTED, "Wapi rejected the application or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Wapi returned malformed or oversized brand data.", True),
        BranchDefinition(DEFECT, "The local app credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
