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

CONNECTOR_ID = "onenet"
PROVIDER = "onenet"
QUERY_DEVICE_DETAIL_OPERATION_ID = "queryDeviceDetail"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

QUERY_DEVICE_DETAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_DEVICE_DETAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "OneNET returned the device detail.", False),
        BranchDefinition(PROVIDER_REJECTED, "OneNET conclusively rejected the query.", True),
        BranchDefinition(INVALID_RESPONSE, "OneNET returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local credentials, product ID, or device name are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
