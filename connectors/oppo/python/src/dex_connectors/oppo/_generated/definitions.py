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

CONNECTOR_ID = "oppo"
PROVIDER = "oppo"
GET_PUSH_QUOTA_OPERATION_ID = "getPushQuota"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_PUSH_QUOTA_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PUSH_QUOTA_OPERATION_ID),
    (
        BranchDefinition(FOUND, "OPPO returned today's quota.", False),
        BranchDefinition(PROVIDER_REJECTED, "OPPO rejected authentication or quota query.", True),
        BranchDefinition(INVALID_RESPONSE, "OPPO returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
