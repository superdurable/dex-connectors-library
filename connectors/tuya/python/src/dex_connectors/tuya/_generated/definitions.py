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

CONNECTOR_ID = "tuya"
PROVIDER = "tuya"
GET_DEVICE_DETAILS_OPERATION_ID = "getDeviceDetails"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_DEVICE_DETAILS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_DEVICE_DETAILS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Tuya returned the device's safe metadata.", False),
        BranchDefinition(NOT_FOUND, "Tuya reported the device does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Tuya rejected token acquisition, signature, permission, or device access.", True),
        BranchDefinition(INVALID_RESPONSE, "Tuya returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
