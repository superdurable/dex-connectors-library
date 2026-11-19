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

CONNECTOR_ID = "youzan"
PROVIDER = "youzan"
GET_SHOP_BASIC_OPERATION_ID = "getShopBasic"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_SHOP_BASIC_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SHOP_BASIC_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Youzan returned the authorized shop.", False),
        BranchDefinition(PROVIDER_REJECTED, "Youzan conclusively rejected the query.", True),
        BranchDefinition(INVALID_RESPONSE, "Youzan returned malformed or oversized shop JSON.", True),
        BranchDefinition(DEFECT, "The local access token is missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=300)),
    ),
)
