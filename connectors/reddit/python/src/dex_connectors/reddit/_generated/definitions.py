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

CONNECTOR_ID = "reddit"
PROVIDER = "reddit"
GET_POST_OPERATION_ID = "getPost"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MISSING = "missing"
PROVIDER_REJECTED = "providerRejected"

GET_POST_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_POST_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Reddit returned the selected post.", False),
        BranchDefinition(MISSING, "Reddit returned no post for the ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Reddit conclusively rejected the read or app access.", True),
        BranchDefinition(INVALID_RESPONSE, "Reddit returned malformed or oversized post JSON.", True),
        BranchDefinition(DEFECT, "The ID, username, or OAuth credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
