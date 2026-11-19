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

CONNECTOR_ID = "textin"
PROVIDER = "textin"
GET_ACCOUNT_BALANCE_OPERATION_ID = "getAccountBalance"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_ACCOUNT_BALANCE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ACCOUNT_BALANCE_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "TextIn returned the balance and payment type.", False),
        BranchDefinition(PROVIDER_REJECTED, "TextIn rejected the credentials or request.", True),
        BranchDefinition(INVALID_RESPONSE, "TextIn returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or connection configuration are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
