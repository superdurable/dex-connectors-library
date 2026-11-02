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

CONNECTOR_ID = "newsapi"
PROVIDER = "newsapi"
SEARCH_ARTICLES_OPERATION_ID = "searchArticles"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"

SEARCH_ARTICLES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_ARTICLES_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "NewsAPI returned one page of matching articles, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "NewsAPI conclusively rejected the search, such as an invalid or exhausted API key, a request the plan does not permit, or an unknown source.", True),
        BranchDefinition(INVALID_RESPONSE, "NewsAPI returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
