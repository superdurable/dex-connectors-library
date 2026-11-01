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

CONNECTOR_ID = "cloudflare"
PROVIDER = "cloudflare"
LIST_ZONES_OPERATION_ID = "listZones"
PURGE_CACHE_BY_URLS_OPERATION_ID = "purgeCacheByURLs"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
PURGED = "purged"

LIST_ZONES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ZONES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List the first page of Cloudflare zones.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

PURGE_CACHE_BY_URLS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, PURGE_CACHE_BY_URLS_OPERATION_ID),
    (
        BranchDefinition(PURGED, "Purge specific URLs from a Cloudflare zone; repeating the same purge has the same cache-removal effect.", False),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
