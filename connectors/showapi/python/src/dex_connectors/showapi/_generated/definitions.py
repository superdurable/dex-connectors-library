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

CONNECTOR_ID = "showapi"
PROVIDER = "showapi"
LOOKUP_AREA_CODES_OPERATION_ID = "lookupAreaCodes"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LOOKUP_AREA_CODES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LOOKUP_AREA_CODES_OPERATION_ID),
    (
        BranchDefinition(FOUND, "ShowAPI returned zero or more area-code matches.", False),
        BranchDefinition(PROVIDER_REJECTED, "ShowAPI rejected credentials, entitlement, quota, or place name.", True),
        BranchDefinition(INVALID_RESPONSE, "ShowAPI returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection configuration or input is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=15), 3, timedelta(seconds=60)),
    ),
)
