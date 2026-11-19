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

CONNECTOR_ID = "ipip"
PROVIDER = "ipip"
LOOKUP_IP_LOCATION_OPERATION_ID = "lookupIPLocation"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LOOKUP_IP_LOCATION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LOOKUP_IP_LOCATION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "IPIP returned default location fields.", False),
        BranchDefinition(PROVIDER_REJECTED, "IPIP rejected the token or query.", True),
        BranchDefinition(INVALID_RESPONSE, "IPIP returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local token or IP address is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
