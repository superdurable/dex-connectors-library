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

CONNECTOR_ID = "shixiseng"
PROVIDER = "shixiseng"
LIST_INDUSTRIES_OPERATION_ID = "listIndustries"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_INDUSTRIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_INDUSTRIES_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Shixiseng returned industry names.", False),
        BranchDefinition(PROVIDER_REJECTED, "Shixiseng rejected merchant credentials or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Shixiseng returned malformed or oversized industry data.", True),
        BranchDefinition(DEFECT, "Local merchant credentials are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
