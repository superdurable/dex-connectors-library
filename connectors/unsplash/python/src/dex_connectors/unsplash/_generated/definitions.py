# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation identities, branches, and Step defaults."""

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

CONNECTOR_ID = "unsplash"
PROVIDER = "unsplash"
SEARCH_PHOTOS_OPERATION_ID = "searchPhotos"
GET_PHOTO_OPERATION_ID = "getPhoto"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"

SEARCH_PHOTOS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_PHOTOS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "Unsplash returned a page of photos, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Unsplash rejected the search or key.", True),
        BranchDefinition(INVALID_RESPONSE, "Unsplash returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_PHOTO_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PHOTO_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Unsplash returned the photo.", False),
        BranchDefinition(NOT_FOUND, "No photo has that ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Unsplash rejected the request or key.", True),
        BranchDefinition(INVALID_RESPONSE, "Unsplash returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
