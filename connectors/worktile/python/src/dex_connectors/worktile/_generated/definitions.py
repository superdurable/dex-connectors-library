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

CONNECTOR_ID = "worktile"
PROVIDER = "worktile"
LIST_PROJECTS_OPERATION_ID = "listProjects"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_PROJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PROJECTS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Worktile returned one page of projects.", False),
        BranchDefinition(PROVIDER_REJECTED, "Worktile rejected the token or project-list request.", True),
        BranchDefinition(INVALID_RESPONSE, "Worktile returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection credentials, input, or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
