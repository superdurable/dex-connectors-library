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

CONNECTOR_ID = "meta-ads-insights"
PROVIDER = "meta-ads-insights"
GET_AD_OPERATION_ID = "getAd"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_AD_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_AD_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Meta returned the ad.", False),
        BranchDefinition(NOT_FOUND, "The ad is missing or inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Meta definitively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Meta returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
