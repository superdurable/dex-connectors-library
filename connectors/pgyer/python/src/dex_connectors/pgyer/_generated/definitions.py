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

CONNECTOR_ID = "pgyer"
PROVIDER = "pgyer"
LIST_UPLOADED_APPS_OPERATION_ID = "listUploadedApps"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_UPLOADED_APPS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_UPLOADED_APPS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Pgyer returned a page of uploaded apps, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Pgyer rejected the API key, permissions, or page.", True),
        BranchDefinition(INVALID_RESPONSE, "Pgyer returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection configuration or input is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
