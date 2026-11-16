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

CONNECTOR_ID = "alapi"
PROVIDER = "alapi"
LOOKUP_IP_LOCATION_OPERATION_ID = "lookupIPLocation"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LOOKUP_IP_LOCATION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LOOKUP_IP_LOCATION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "ALAPI returned the IP location.", False),
        BranchDefinition(PROVIDER_REJECTED, "ALAPI rejected credentials, quota, or the query.", True),
        BranchDefinition(INVALID_RESPONSE, "ALAPI returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local credentials or IP address are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
