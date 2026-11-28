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

CONNECTOR_ID = "hacker-news-daily"
PROVIDER = "hacker-news"
LIST_STORY_IDS_OPERATION_ID = "listStoryIDs"
GET_ITEM_OPERATION_ID = "getItem"
GET_USER_OPERATION_ID = "getUser"
GET_ARTICLE_THREAD_OPERATION_ID = "getArticleThread"
SEARCH_STORIES_OPERATION_ID = "searchStories"
SEARCH_ITEMS_OPERATION_ID = "searchItems"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"

LIST_STORY_IDS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_STORY_IDS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "The feed snapshot was read.", False),
        BranchDefinition(PROVIDER_REJECTED, "Hacker News rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Hacker News returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 0, timedelta(seconds=3900)),
    ),
)

GET_ITEM_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ITEM_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The public item was read.", False),
        BranchDefinition(NOT_FOUND, "The item is missing, deleted, or dead.", True),
        BranchDefinition(PROVIDER_REJECTED, "Hacker News rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Hacker News returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 0, timedelta(seconds=3900)),
    ),
)

GET_USER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_USER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The public user was read.", False),
        BranchDefinition(NOT_FOUND, "The user has no public profile.", True),
        BranchDefinition(PROVIDER_REJECTED, "Hacker News rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Hacker News returned malformed or oversized user data.", True),
        BranchDefinition(DEFECT, "The username or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_ARTICLE_THREAD_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ARTICLE_THREAD_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The article and its indexed comments were read.", False),
        BranchDefinition(NOT_FOUND, "The article was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "Hacker News Search rejected the article read.", True),
        BranchDefinition(INVALID_RESPONSE, "Hacker News Search returned malformed or oversized thread data.", True),
        BranchDefinition(DEFECT, "The article ID or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SEARCH_STORIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_STORIES_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of matching stories was read; it can be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Hacker News Search rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Hacker News Search returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 0, timedelta(seconds=3900)),
    ),
)

SEARCH_ITEMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_ITEMS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of public items was read; it can be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Hacker News Search rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Hacker News Search returned malformed or oversized items.", True),
        BranchDefinition(DEFECT, "Search filters or connection configuration are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
