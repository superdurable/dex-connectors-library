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

CONNECTOR_ID = "workday"
PROVIDER = "workday"
GET_WORKER_OPERATION_ID = "getWorker"
LIST_WORKERS_OPERATION_ID = "listWorkers"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_WORKER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_WORKER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Workday returned the requested worker.", False),
        BranchDefinition(NOT_FOUND, "No accessible worker has this WID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Workday conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Workday returned malformed or oversized worker JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_WORKERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_WORKERS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Workday returned the requested worker page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Workday conclusively rejected the search.", True),
        BranchDefinition(INVALID_RESPONSE, "Workday returned malformed or oversized worker JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
