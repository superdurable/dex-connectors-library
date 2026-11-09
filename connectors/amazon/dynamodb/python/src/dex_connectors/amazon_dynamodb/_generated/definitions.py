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

CONNECTOR_ID = "amazon-dynamodb"
PROVIDER = "amazon-dynamodb"
GET_ITEM_OPERATION_ID = "getItem"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MISSING = "missing"
PROVIDER_REJECTED = "providerRejected"

GET_ITEM_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ITEM_OPERATION_ID),
    (
        BranchDefinition(FOUND, "DynamoDB returned the item.", False),
        BranchDefinition(MISSING, "The table has no item with that key.", True),
        BranchDefinition(PROVIDER_REJECTED, "DynamoDB conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "DynamoDB returned malformed or oversized item JSON.", True),
        BranchDefinition(DEFECT, "The table, key, endpoint, Region, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
