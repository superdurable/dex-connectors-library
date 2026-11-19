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

CONNECTOR_ID = "huaweicloud"
PROVIDER = "huaweicloud"
LIST_ACCESSIBLE_PROJECTS_OPERATION_ID = "listAccessibleProjects"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_ACCESSIBLE_PROJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ACCESSIBLE_PROJECTS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Huawei Cloud returned the user's accessible projects.", False),
        BranchDefinition(PROVIDER_REJECTED, "Huawei Cloud rejected token acquisition or project access.", True),
        BranchDefinition(INVALID_RESPONSE, "Huawei Cloud returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection credentials or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
