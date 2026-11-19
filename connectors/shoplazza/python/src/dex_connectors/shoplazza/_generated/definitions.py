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

CONNECTOR_ID = "shoplazza"
PROVIDER = "shoplazza"
GET_SHOP_OPERATION_ID = "getShop"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_SHOP_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SHOP_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Shoplazza returned shop metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "Shoplazza rejected the token, scope or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Shoplazza returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
