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

CONNECTOR_ID = "tingyun"
PROVIDER = "tingyun"
LIST_APPLICATIONS_OPERATION_ID = "listApplications"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_APPLICATIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_APPLICATIONS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Tingyun returned mobile application metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "Tingyun rejected authentication or access.", True),
        BranchDefinition(INVALID_RESPONSE, "Tingyun returned malformed or oversized metadata.", True),
        BranchDefinition(DEFECT, "Local host or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
