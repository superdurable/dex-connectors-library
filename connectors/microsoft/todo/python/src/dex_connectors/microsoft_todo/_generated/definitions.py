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

CONNECTOR_ID = "microsoft-todo"
PROVIDER = "microsoft"
LIST_TASK_LISTS_OPERATION_ID = "listTaskLists"
LIST_TASKS_OPERATION_ID = "listTasks"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_TASK_LISTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TASK_LISTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Graph returned task lists.", False),
        BranchDefinition(PROVIDER_REJECTED, "Graph conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Graph returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_TASKS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TASKS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Graph returned the tasks.", False),
        BranchDefinition(NOT_FOUND, "The task list was absent or inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Graph conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Graph returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
