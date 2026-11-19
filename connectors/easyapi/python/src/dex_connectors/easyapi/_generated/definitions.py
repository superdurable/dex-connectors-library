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

CONNECTOR_ID = "easyapi"
PROVIDER = "easyapi"
QUERY_SHOP_OPERATION_ID = "queryShop"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

QUERY_SHOP_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_SHOP_OPERATION_ID),
    (
        BranchDefinition(FOUND, "EasyAPI returned the company's invoicing service configuration.", False),
        BranchDefinition(PROVIDER_REJECTED, "EasyAPI rejected the credentials or tax number.", True),
        BranchDefinition(INVALID_RESPONSE, "EasyAPI returned malformed or oversized company data.", True),
        BranchDefinition(DEFECT, "The local tax number or connection is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
