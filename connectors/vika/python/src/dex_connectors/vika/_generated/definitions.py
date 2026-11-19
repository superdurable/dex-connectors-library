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

CONNECTOR_ID = "vika"
PROVIDER = "vika"
LIST_SPACES_OPERATION_ID = "listSpaces"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_SPACES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SPACES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Vika returned the authorized workspace list, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Vika rejected the token or permissions.", True),
        BranchDefinition(INVALID_RESPONSE, "Vika returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection configuration or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
