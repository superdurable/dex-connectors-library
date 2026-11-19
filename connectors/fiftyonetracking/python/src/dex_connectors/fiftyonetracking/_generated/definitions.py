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

CONNECTOR_ID = "fiftyonetracking"
PROVIDER = "fiftyonetracking"
GET_SHIPMENT_STATUS_COUNTS_OPERATION_ID = "getShipmentStatusCounts"
COUNTED = "counted"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_SHIPMENT_STATUS_COUNTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SHIPMENT_STATUS_COUNTS_OPERATION_ID),
    (
        BranchDefinition(COUNTED, "51Tracking returned aggregate parcel status counts.", False),
        BranchDefinition(PROVIDER_REJECTED, "51Tracking rejected the key or request.", True),
        BranchDefinition(INVALID_RESPONSE, "51Tracking returned malformed or oversized status data.", True),
        BranchDefinition(DEFECT, "Local API key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
