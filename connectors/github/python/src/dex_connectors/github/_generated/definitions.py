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

CONNECTOR_ID = "github"
PROVIDER = "github"
GET_AUTHENTICATED_PROFILE_OPERATION_ID = "getAuthenticatedProfile"
LIST_PUBLIC_REPOSITORIES_OPERATION_ID = "listPublicRepositories"
LIST_MERGED_PULL_REQUESTS_OPERATION_ID = "listMergedPullRequests"
LIST_PULL_REQUEST_FILES_OPERATION_ID = "listPullRequestFiles"
LIST_RELEASES_OPERATION_ID = "listReleases"
LIST_COMMITS_OPERATION_ID = "listCommits"
AUTHORIZATION_REVOKED = "authorizationRevoked"
DEFECT = "defect"
INSUFFICIENT_SCOPE = "insufficientScope"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROFILE_LOADED = "profileLoaded"
PROVIDER_REJECTED = "providerRejected"
REPOSITORIES_LOADED = "repositoriesLoaded"
VERIFIED_EMAIL_REQUIRED = "verifiedEmailRequired"

GET_AUTHENTICATED_PROFILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_AUTHENTICATED_PROFILE_OPERATION_ID),
    (
        BranchDefinition(PROFILE_LOADED, "The authenticated profile and primary verified email were loaded.", False),
        BranchDefinition(VERIFIED_EMAIL_REQUIRED, "A primary verified email was not available.", True),
        BranchDefinition(INSUFFICIENT_SCOPE, "The OAuth grant does not match the required scopes.", True),
        BranchDefinition(AUTHORIZATION_REVOKED, "The OAuth grant is invalid or revoked.", True),
        BranchDefinition(NOT_FOUND, "The authenticated account was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "GitHub conclusively rejected the profile query.", True),
        BranchDefinition(INVALID_RESPONSE, "GitHub returned an invalid or oversized profile response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_PUBLIC_REPOSITORIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PUBLIC_REPOSITORIES_OPERATION_ID),
    (
        BranchDefinition(REPOSITORIES_LOADED, "Public repositories were loaded and bounded.", False),
        BranchDefinition(INSUFFICIENT_SCOPE, "The OAuth grant does not match the required scopes.", True),
        BranchDefinition(AUTHORIZATION_REVOKED, "The OAuth grant is invalid or revoked.", True),
        BranchDefinition(NOT_FOUND, "The requested GitHub account was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "GitHub conclusively rejected the repository query.", True),
        BranchDefinition(INVALID_RESPONSE, "GitHub returned an invalid or oversized repository response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_MERGED_PULL_REQUESTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MERGED_PULL_REQUESTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One bounded page of merged pull requests was loaded.", False),
        BranchDefinition(INSUFFICIENT_SCOPE, "The OAuth grant does not match the required scopes.", True),
        BranchDefinition(AUTHORIZATION_REVOKED, "The OAuth grant is invalid or revoked.", True),
        BranchDefinition(NOT_FOUND, "The requested GitHub resource was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "GitHub conclusively rejected the search, including a repository it cannot search.", True),
        BranchDefinition(INVALID_RESPONSE, "GitHub returned an invalid or oversized search response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 0, timedelta(seconds=3900)),
    ),
)

LIST_PULL_REQUEST_FILES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PULL_REQUEST_FILES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One bounded page of changed files was loaded.", False),
        BranchDefinition(INSUFFICIENT_SCOPE, "The OAuth grant does not match the required scopes.", True),
        BranchDefinition(AUTHORIZATION_REVOKED, "The OAuth grant is invalid or revoked.", True),
        BranchDefinition(NOT_FOUND, "The repository or pull request was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "GitHub conclusively rejected the changed-file query.", True),
        BranchDefinition(INVALID_RESPONSE, "GitHub returned an invalid or oversized changed-file response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 0, timedelta(seconds=3900)),
    ),
)

LIST_RELEASES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_RELEASES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One bounded page of releases was loaded, possibly empty.", False),
        BranchDefinition(INSUFFICIENT_SCOPE, "The OAuth grant does not match the required scopes.", True),
        BranchDefinition(AUTHORIZATION_REVOKED, "The OAuth grant is invalid or revoked.", True),
        BranchDefinition(NOT_FOUND, "The repository was not found or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "GitHub conclusively rejected the releases query.", True),
        BranchDefinition(INVALID_RESPONSE, "GitHub returned an invalid or oversized releases response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 0, timedelta(seconds=3900)),
    ),
)

LIST_COMMITS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_COMMITS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One bounded page of commits was loaded; an empty repository has no commits.", False),
        BranchDefinition(INSUFFICIENT_SCOPE, "The OAuth grant does not match the required scopes.", True),
        BranchDefinition(AUTHORIZATION_REVOKED, "The OAuth grant is invalid or revoked.", True),
        BranchDefinition(NOT_FOUND, "The repository or starting ref was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "GitHub conclusively rejected the commit query.", True),
        BranchDefinition(INVALID_RESPONSE, "GitHub returned an invalid or oversized commit response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 0, timedelta(seconds=3900)),
    ),
)
