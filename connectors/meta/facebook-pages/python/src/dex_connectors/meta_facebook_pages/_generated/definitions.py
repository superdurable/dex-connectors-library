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

CONNECTOR_ID = "facebook-pages"
PROVIDER = "facebook-pages"
LIST_POSTS_OPERATION_ID = "listPosts"
GET_POST_OPERATION_ID = "getPost"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_POSTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_POSTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Meta returned one Page feed page.", False),
        BranchDefinition(NOT_FOUND, "The Page is missing or inaccessible.", True),
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

GET_POST_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_POST_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Meta returned the Page post.", False),
        BranchDefinition(NOT_FOUND, "The post is missing or inaccessible.", True),
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
