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

CONNECTOR_ID = "linkedin"
PROVIDER = "linkedin"
GET_AUTHENTICATED_PROFILE_OPERATION_ID = "getAuthenticatedProfile"
AUTHORIZATION_REVOKED = "authorizationRevoked"
DEFECT = "defect"
INSUFFICIENT_SCOPE = "insufficientScope"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROFILE_LOADED = "profileLoaded"
PROVIDER_REJECTED = "providerRejected"
VERIFIED_EMAIL_REQUIRED = "verifiedEmailRequired"

GET_AUTHENTICATED_PROFILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_AUTHENTICATED_PROFILE_OPERATION_ID),
    (
        BranchDefinition(PROFILE_LOADED, "The authenticated profile and verified email were loaded.", False),
        BranchDefinition(VERIFIED_EMAIL_REQUIRED, "A verified email was not available.", True),
        BranchDefinition(INSUFFICIENT_SCOPE, "The OpenID Connect grant lacks required authorization.", True),
        BranchDefinition(AUTHORIZATION_REVOKED, "The OpenID Connect grant is invalid or revoked.", True),
        BranchDefinition(NOT_FOUND, "The authenticated member was not found.", True),
        BranchDefinition(PROVIDER_REJECTED, "LinkedIn conclusively rejected the profile query.", True),
        BranchDefinition(INVALID_RESPONSE, "LinkedIn returned an invalid or oversized profile response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
