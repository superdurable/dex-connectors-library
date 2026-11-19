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

CONNECTOR_ID = "fofa"
PROVIDER = "fofa"
SEARCH_DOMAIN_ASSETS_OPERATION_ID = "searchDomainAssets"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

SEARCH_DOMAIN_ASSETS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_DOMAIN_ASSETS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "FOFA returned indexed hosts for the domain.", False),
        BranchDefinition(PROVIDER_REJECTED, "FOFA rejected the Key or domain query.", True),
        BranchDefinition(INVALID_RESPONSE, "FOFA returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local domain or API Key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=10), 2, timedelta(seconds=60)),
    ),
)
