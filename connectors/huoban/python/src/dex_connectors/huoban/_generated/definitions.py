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

CONNECTOR_ID = "huoban"
PROVIDER = "huoban"
LIST_SPACES_OPERATION_ID = "listSpaces"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_SPACES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SPACES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Huoban returned authorized workspace metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "Huoban rejected authentication or access.", True),
        BranchDefinition(INVALID_RESPONSE, "Huoban returned malformed or oversized metadata.", True),
        BranchDefinition(DEFECT, "Local API Key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
