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

CONNECTOR_ID = "honor"
PROVIDER = "honor"
LOOKUP_APP_ID_OPERATION_ID = "lookupAppID"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LOOKUP_APP_ID_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LOOKUP_APP_ID_OPERATION_ID),
    (
        BranchDefinition(FOUND, "HONOR returned the owned app ID.", False),
        BranchDefinition(NOT_FOUND, "No owned app matches the package name.", True),
        BranchDefinition(PROVIDER_REJECTED, "HONOR rejected the credentials or request.", True),
        BranchDefinition(INVALID_RESPONSE, "HONOR returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials, package name, or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
