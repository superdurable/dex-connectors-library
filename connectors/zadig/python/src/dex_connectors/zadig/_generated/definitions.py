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

CONNECTOR_ID = "zadig"
PROVIDER = "zadig"
LIST_PROJECTS_OPERATION_ID = "listProjects"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_PROJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PROJECTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Zadig returned a project page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zadig rejected the token or project access.", True),
        BranchDefinition(INVALID_RESPONSE, "Zadig returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or page options are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
