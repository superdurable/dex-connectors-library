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

CONNECTOR_ID = "seventeentrack"
PROVIDER = "seventeentrack"
LIST_TRACKED_SHIPMENTS_OPERATION_ID = "listTrackedShipments"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_TRACKED_SHIPMENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TRACKED_SHIPMENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "17TRACK returned a page of registered shipments, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "17TRACK rejected the key or request.", True),
        BranchDefinition(INVALID_RESPONSE, "17TRACK returned malformed or oversized tracking data.", True),
        BranchDefinition(DEFECT, "Local page number or key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
