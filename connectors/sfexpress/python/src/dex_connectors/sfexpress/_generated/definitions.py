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

CONNECTOR_ID = "sfexpress"
PROVIDER = "sfexpress"
QUERY_ROUTES_OPERATION_ID = "queryRoutes"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

QUERY_ROUTES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_ROUTES_OPERATION_ID),
    (
        BranchDefinition(FOUND, "SF Express returned route events for the waybill, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "SF Express rejected the credentials, permission, or waybill.", True),
        BranchDefinition(INVALID_RESPONSE, "SF Express returned malformed or oversized route data.", True),
        BranchDefinition(DEFECT, "The local waybill or connection is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
