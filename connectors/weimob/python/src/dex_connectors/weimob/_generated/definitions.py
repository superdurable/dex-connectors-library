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

CONNECTOR_ID = "weimob"
PROVIDER = "weimob"
GET_STORE_BASIC_OPERATION_ID = "getStoreBasic"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_STORE_BASIC_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_STORE_BASIC_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Weimob returned the authorized WOS store.", False),
        BranchDefinition(PROVIDER_REJECTED, "Weimob conclusively rejected token acquisition or the store query.", True),
        BranchDefinition(INVALID_RESPONSE, "Weimob returned malformed or oversized token or store JSON.", True),
        BranchDefinition(DEFECT, "Local client credentials or store ID are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
