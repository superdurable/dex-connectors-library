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

CONNECTOR_ID = "kunlun"
PROVIDER = "kunlun"
GET_TASK_STATUS_OPERATION_ID = "getTaskStatus"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_TASK_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TASK_STATUS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Mureka returned the song task status.", False),
        BranchDefinition(NOT_FOUND, "Mureka did not find the song task.", True),
        BranchDefinition(PROVIDER_REJECTED, "Mureka rejected authentication or task access.", True),
        BranchDefinition(INVALID_RESPONSE, "Mureka returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local task ID, credentials or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
