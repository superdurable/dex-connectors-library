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

CONNECTOR_ID = "winit"
PROVIDER = "winit"
LIST_WAREHOUSES_OPERATION_ID = "listWarehouses"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_WAREHOUSES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_WAREHOUSES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Winit returned the authorized warehouse catalog, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Winit rejected credentials, authorization, or the query.", True),
        BranchDefinition(INVALID_RESPONSE, "Winit returned malformed or oversized warehouse data.", True),
        BranchDefinition(DEFECT, "Local configuration or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
