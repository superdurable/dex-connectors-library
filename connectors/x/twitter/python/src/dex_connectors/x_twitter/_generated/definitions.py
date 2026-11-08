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

CONNECTOR_ID = "x-twitter"
PROVIDER = "x-twitter"
GET_POST_OPERATION_ID = "getPost"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_POST_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_POST_OPERATION_ID),
    (
        BranchDefinition(FOUND, "X returned the requested Post.", False),
        BranchDefinition(NOT_FOUND, "X did not expose that Post.", True),
        BranchDefinition(PROVIDER_REJECTED, "X conclusively rejected the lookup or app token.", True),
        BranchDefinition(INVALID_RESPONSE, "X returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local Post ID or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
