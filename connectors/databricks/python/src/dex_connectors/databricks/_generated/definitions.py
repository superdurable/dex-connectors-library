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

CONNECTOR_ID = "databricks"
PROVIDER = "databricks"
GET_JOB_RUN_OPERATION_ID = "getJobRun"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_JOB_RUN_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_JOB_RUN_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Databricks returned the job run.", False),
        BranchDefinition(NOT_FOUND, "Databricks did not expose the job run.", True),
        BranchDefinition(PROVIDER_REJECTED, "Databricks conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Databricks returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
