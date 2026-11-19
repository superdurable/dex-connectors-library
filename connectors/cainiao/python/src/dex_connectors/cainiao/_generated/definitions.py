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

CONNECTOR_ID = "cainiao"
PROVIDER = "cainiao"
QUERY_WAYBILL_TRACE_OPERATION_ID = "queryWaybillTrace"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

QUERY_WAYBILL_TRACE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_WAYBILL_TRACE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "TOP returned the waybill's logistics events.", False),
        BranchDefinition(PROVIDER_REJECTED, "TOP conclusively rejected the query or waybill.", True),
        BranchDefinition(INVALID_RESPONSE, "TOP returned malformed or oversized trace JSON.", True),
        BranchDefinition(DEFECT, "The local credentials or input are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
