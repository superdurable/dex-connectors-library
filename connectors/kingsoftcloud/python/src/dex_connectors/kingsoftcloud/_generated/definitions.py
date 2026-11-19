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

CONNECTOR_ID = "kingsoftcloud"
PROVIDER = "kingsoftcloud"
LIST_USERS_OPERATION_ID = "listUsers"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_USERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_USERS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "IAM returned one user page, which may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "IAM rejected the access key, permissions, or request.", True),
        BranchDefinition(INVALID_RESPONSE, "IAM returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
