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

CONNECTOR_ID = "apifox"
PROVIDER = "apifox"
GET_PROJECT_API_SUMMARY_OPERATION_ID = "getProjectAPISummary"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_PROJECT_API_SUMMARY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PROJECT_API_SUMMARY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Apifox exported the project definition.", False),
        BranchDefinition(PROVIDER_REJECTED, "Apifox rejected the token, project, version, or permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Apifox returned malformed or oversized OpenAPI data.", True),
        BranchDefinition(DEFECT, "The local token or project ID is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
