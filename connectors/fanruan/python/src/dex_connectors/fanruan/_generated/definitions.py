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

CONNECTOR_ID = "fanruan"
PROVIDER = "fanruan"
LIST_APPS_PAGE_OPERATION_ID = "listAppsPage"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_APPS_PAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_APPS_PAGE_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Jiandaoyun returned one page of applications.", False),
        BranchDefinition(PROVIDER_REJECTED, "Jiandaoyun rejected the API Key or permissions.", True),
        BranchDefinition(INVALID_RESPONSE, "Jiandaoyun returned malformed or oversized app metadata.", True),
        BranchDefinition(DEFECT, "Local pagination, credentials or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
