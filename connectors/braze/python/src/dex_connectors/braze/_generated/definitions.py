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

CONNECTOR_ID = "braze"
PROVIDER = "braze"
GET_USER_PROFILE_OPERATION_ID = "getUserProfile"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_USER_PROFILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_USER_PROFILE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Braze returned the user profile.", False),
        BranchDefinition(NOT_FOUND, "Braze returned no user for the external ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Braze conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Braze returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
