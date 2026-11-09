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

CONNECTOR_ID = "azure-devops"
PROVIDER = "azure-devops"
LIST_PROJECTS_OPERATION_ID = "listProjects"
GET_WORK_ITEM_OPERATION_ID = "getWorkItem"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_PROJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PROJECTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Azure DevOps returned accessible projects.", False),
        BranchDefinition(PROVIDER_REJECTED, "Azure DevOps conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Azure DevOps returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Unavailable or invalid connection credentials.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_WORK_ITEM_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_WORK_ITEM_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Azure DevOps returned the selected work item.", False),
        BranchDefinition(NOT_FOUND, "No work item matches this ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Azure DevOps conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Azure DevOps returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Invalid work item ID or unavailable credentials.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
