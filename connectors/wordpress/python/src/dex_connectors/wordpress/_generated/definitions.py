# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RepeatOnUnknown,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "wordpress"
PROVIDER = "wordpress"
CREATE_POST_OPERATION_ID = "createPost"
GET_POST_OPERATION_ID = "getPost"
LIST_POSTS_OPERATION_ID = "listPosts"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"

CREATE_POST_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_POST_OPERATION_ID),
    (
        BranchDefinition(CREATED, "WordPress created the post and returned its numeric ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "WordPress conclusively rejected the new post.", True),
        BranchDefinition(UNCERTAIN, "WordPress may have created the post, but the send cap was reached or WordPress returned a post ID without a complete confirmation; inspect Posts before creating another.", True),
        BranchDefinition(DEFECT, "Local post input, connection credentials, or dispatch fence is invalid before any send.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.LIMITED,
    max_sends=3,
)

GET_POST_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_POST_OPERATION_ID),
    (
        BranchDefinition(FOUND, "WordPress returned the published post.", False),
        BranchDefinition(NOT_FOUND, "The post is missing or not visible to this connection.", True),
        BranchDefinition(PROVIDER_REJECTED, "WordPress conclusively rejected the post lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "WordPress returned malformed or oversized post data.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_POSTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_POSTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "WordPress returned one post page, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "WordPress conclusively rejected the post list.", True),
        BranchDefinition(INVALID_RESPONSE, "WordPress returned malformed or oversized post-page data.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
