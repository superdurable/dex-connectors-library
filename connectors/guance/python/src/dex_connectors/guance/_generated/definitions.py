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

CONNECTOR_ID = "guance"
PROVIDER = "guance"
LIST_DASHBOARDS_PAGE_OPERATION_ID = "listDashboardsPage"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_DASHBOARDS_PAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DASHBOARDS_PAGE_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Guance returned dashboard metadata and pagination.", False),
        BranchDefinition(PROVIDER_REJECTED, "Guance rejected authentication or access.", True),
        BranchDefinition(INVALID_RESPONSE, "Guance returned malformed or oversized metadata.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
