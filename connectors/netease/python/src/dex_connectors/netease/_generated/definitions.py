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

CONNECTOR_ID = "netease"
PROVIDER = "netease"
GET_USER_PROFILE_OPERATION_ID = "getUserProfile"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_USER_PROFILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_USER_PROFILE_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "Yunxin returned the user's profile.", False),
        BranchDefinition(NOT_FOUND, "The account or its profile does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Yunxin rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Yunxin returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
