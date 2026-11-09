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

CONNECTOR_ID = "upstash-redis"
PROVIDER = "upstash-redis"
GET_KEY_OPERATION_ID = "getKey"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MISSING = "missing"
PROVIDER_REJECTED = "providerRejected"

GET_KEY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_KEY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The key exists and has a string value.", False),
        BranchDefinition(MISSING, "The key does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Upstash conclusively rejected the command or token.", True),
        BranchDefinition(INVALID_RESPONSE, "Upstash returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "The key, endpoint, or token is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
