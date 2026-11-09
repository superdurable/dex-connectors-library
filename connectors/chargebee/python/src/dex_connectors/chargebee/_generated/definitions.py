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

CONNECTOR_ID = "chargebee"
PROVIDER = "chargebee"
GET_SUBSCRIPTION_OPERATION_ID = "getSubscription"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_SUBSCRIPTION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SUBSCRIPTION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Chargebee returned the subscription.", False),
        BranchDefinition(NOT_FOUND, "Chargebee did not expose the subscription.", True),
        BranchDefinition(PROVIDER_REJECTED, "Chargebee conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Chargebee returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
