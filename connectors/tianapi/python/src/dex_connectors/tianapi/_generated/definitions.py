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

CONNECTOR_ID = "tianapi"
PROVIDER = "tianapi"
GET_HOLIDAY_STATUS_OPERATION_ID = "getHolidayStatus"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_HOLIDAY_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_HOLIDAY_STATUS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "TianAPI returned one date's holiday status.", False),
        BranchDefinition(PROVIDER_REJECTED, "TianAPI rejected the request or key.", True),
        BranchDefinition(INVALID_RESPONSE, "TianAPI returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local input or key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
