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

CONNECTOR_ID = "vercel"
PROVIDER = "vercel"
LIST_DEPLOYMENTS_OPERATION_ID = "listDeployments"
GET_DEPLOYMENT_OPERATION_ID = "getDeployment"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_DEPLOYMENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DEPLOYMENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List deployments visible to a personal account or team.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_DEPLOYMENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_DEPLOYMENT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Read a deployment by Vercel deployment ID or hostname.", False),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
