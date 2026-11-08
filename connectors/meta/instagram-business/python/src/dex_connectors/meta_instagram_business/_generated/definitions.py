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

CONNECTOR_ID = "instagram-business"
PROVIDER = "instagram-business"
LIST_MEDIA_OPERATION_ID = "listMedia"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_MEDIA_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MEDIA_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Meta returned one media page.", False),
        BranchDefinition(NOT_FOUND, "The account is missing or inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Meta definitively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Meta returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
