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

CONNECTOR_ID = "bilibili"
PROVIDER = "bilibili"
GET_AUTHORIZED_USER_OPERATION_ID = "getAuthorizedUser"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_AUTHORIZED_USER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_AUTHORIZED_USER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Bilibili returned the authorized user's public identity.", False),
        BranchDefinition(PROVIDER_REJECTED, "Bilibili conclusively rejected the account lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Bilibili returned malformed or oversized account JSON.", True),
        BranchDefinition(DEFECT, "The local credentials are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
