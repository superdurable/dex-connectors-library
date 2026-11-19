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

CONNECTOR_ID = "fit2cloud"
PROVIDER = "fit2cloud"
LIST_POSTS_OPERATION_ID = "listPosts"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_POSTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_POSTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Halo returned an article page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Halo rejected the token or access.", True),
        BranchDefinition(INVALID_RESPONSE, "Halo returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or pagination are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
