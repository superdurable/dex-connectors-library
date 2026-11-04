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

CONNECTOR_ID = "tmdb"
PROVIDER = "tmdb"
SEARCH_MOVIES_OPERATION_ID = "searchMovies"
GET_MOVIE_OPERATION_ID = "getMovie"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"

SEARCH_MOVIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_MOVIES_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "TMDB returned the requested result.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_MOVIE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_MOVIE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "TMDB returned the requested result.", False),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
