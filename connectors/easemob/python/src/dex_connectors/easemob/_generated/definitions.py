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

CONNECTOR_ID = "easemob"
PROVIDER = "easemob"
GET_USER_DETAIL_OPERATION_ID = "getUserDetail"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_USER_DETAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_USER_DETAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Easemob returned the user detail.", False),
        BranchDefinition(PROVIDER_REJECTED, "Easemob conclusively rejected the query.", True),
        BranchDefinition(INVALID_RESPONSE, "Easemob returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local credentials or user ID are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
