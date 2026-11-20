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

CONNECTOR_ID = "gowu"
PROVIDER = "gowu"
GET_ACCESS_CLIENT_OPERATION_ID = "getAccessClient"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_ACCESS_CLIENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ACCESS_CLIENT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Gowu returned the authorized application's information.", False),
        BranchDefinition(PROVIDER_REJECTED, "Gowu rejected the key or permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Gowu returned malformed or oversized application data.", True),
        BranchDefinition(DEFECT, "The local connection is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
