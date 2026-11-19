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

CONNECTOR_ID = "getui"
PROVIDER = "getui"
GET_DAILY_PUSH_REPORT_OPERATION_ID = "getDailyPushReport"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_DAILY_PUSH_REPORT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_DAILY_PUSH_REPORT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Getui returned the daily push totals.", False),
        BranchDefinition(PROVIDER_REJECTED, "Getui rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Getui returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 4, timedelta(seconds=600)),
    ),
)
