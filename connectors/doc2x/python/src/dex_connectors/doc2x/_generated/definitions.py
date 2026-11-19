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

CONNECTOR_ID = "doc2x"
PROVIDER = "doc2x"
GET_PARSE_STATUS_OPERATION_ID = "getParseStatus"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_PARSE_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PARSE_STATUS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Doc2X returned the current task state.", False),
        BranchDefinition(PROVIDER_REJECTED, "Doc2X rejected the key or task identifier.", True),
        BranchDefinition(INVALID_RESPONSE, "Doc2X returned malformed or oversized status data.", True),
        BranchDefinition(DEFECT, "The local API Key or task identifier is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
