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
PUBLISH_TEXT_POST_OPERATION_ID = "publishTextPost"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
PUBLISHED = "published"
UNCERTAIN = "uncertain"

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

PUBLISH_TEXT_POST_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, PUBLISH_TEXT_POST_OPERATION_ID),
    (
        BranchDefinition(PUBLISHED, "Meta returned the published post ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "Meta definitively rejected the post.", True),
        BranchDefinition(UNCERTAIN, "The publish outcome is unknown; inspect the Page before retrying manually.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
