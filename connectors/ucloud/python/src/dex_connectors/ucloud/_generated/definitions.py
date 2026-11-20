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

CONNECTOR_ID = "ucloud"
PROVIDER = "ucloud"
GET_REGION_OPERATION_ID = "getRegion"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_REGION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_REGION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "UCloud returned zero or more accessible regions and zones.", False),
        BranchDefinition(PROVIDER_REJECTED, "UCloud rejected the key or request with a terminal RetCode.", True),
        BranchDefinition(INVALID_RESPONSE, "UCloud returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Connection credentials or local configuration are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
