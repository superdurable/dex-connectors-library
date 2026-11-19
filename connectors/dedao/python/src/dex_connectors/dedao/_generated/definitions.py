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

CONNECTOR_ID = "dedao"
PROVIDER = "dedao"
GET_NOTE_TASK_STATUS_OPERATION_ID = "getNoteTaskStatus"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_NOTE_TASK_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_NOTE_TASK_STATUS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Get Note returned the task status.", False),
        BranchDefinition(PROVIDER_REJECTED, "Get Note rejected the credentials or task request.", True),
        BranchDefinition(INVALID_RESPONSE, "Get Note returned malformed or oversized status data.", True),
        BranchDefinition(DEFECT, "Local credentials or task ID are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
