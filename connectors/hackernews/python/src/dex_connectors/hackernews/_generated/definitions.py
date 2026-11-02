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
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

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
