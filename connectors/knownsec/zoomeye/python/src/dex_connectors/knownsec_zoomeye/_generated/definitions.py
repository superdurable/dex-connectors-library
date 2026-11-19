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

CONNECTOR_ID = "zoomeye"
PROVIDER = "zoomeye"
GET_ACCOUNT_QUOTA_OPERATION_ID = "getAccountQuota"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_ACCOUNT_QUOTA_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ACCOUNT_QUOTA_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "ZoomEye returned account subscription and points.", False),
        BranchDefinition(PROVIDER_REJECTED, "ZoomEye rejected the key or account access.", True),
        BranchDefinition(INVALID_RESPONSE, "ZoomEye returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or connection configuration are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
