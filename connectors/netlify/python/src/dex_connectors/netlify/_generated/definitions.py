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

CONNECTOR_ID = "netlify"
PROVIDER = "netlify"
LIST_SITES_OPERATION_ID = "listSites"
LIST_DEPLOYS_OPERATION_ID = "listDeploys"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_SITES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SITES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List sites visible to the Netlify token.", False),
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

LIST_DEPLOYS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DEPLOYS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List deploys for one Netlify site.", False),
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
