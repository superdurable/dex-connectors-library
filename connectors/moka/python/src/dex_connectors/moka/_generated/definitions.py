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

CONNECTOR_ID = "moka"
PROVIDER = "moka"
LIST_JOB_FUNCTIONS_OPERATION_ID = "listJobFunctions"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_JOB_FUNCTIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_JOB_FUNCTIONS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Moka returned the job function list.", False),
        BranchDefinition(PROVIDER_REJECTED, "Moka rejected the job-function request.", True),
        BranchDefinition(INVALID_RESPONSE, "Moka returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection credentials or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
