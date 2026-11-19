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

CONNECTOR_ID = "pingcap"
PROVIDER = "pingcap"
LIST_PROJECTS_OPERATION_ID = "listProjects"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_PROJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PROJECTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "TiDB Cloud returned a page of accessible projects.", False),
        BranchDefinition(PROVIDER_REJECTED, "TiDB Cloud rejected authentication, permission, or request.", True),
        BranchDefinition(INVALID_RESPONSE, "TiDB Cloud returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
