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

CONNECTOR_ID = "yanwen"
PROVIDER = "yanwen"
LIST_WAREHOUSES_OPERATION_ID = "listWarehouses"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_WAREHOUSES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_WAREHOUSES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Yanwen returned a warehouse list, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Yanwen rejected the account or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Yanwen returned malformed or oversized warehouse data.", True),
        BranchDefinition(DEFECT, "The local connection is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
