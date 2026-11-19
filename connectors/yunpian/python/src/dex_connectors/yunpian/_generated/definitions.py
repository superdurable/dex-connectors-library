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

CONNECTOR_ID = "yunpian"
PROVIDER = "yunpian"
GET_ACCOUNT_BALANCE_OPERATION_ID = "getAccountBalance"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
READ = "read"

GET_ACCOUNT_BALANCE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ACCOUNT_BALANCE_OPERATION_ID),
    (
        BranchDefinition(READ, "Yunpian returned the account balance.", False),
        BranchDefinition(PROVIDER_REJECTED, "Yunpian conclusively rejected the query.", True),
        BranchDefinition(INVALID_RESPONSE, "Yunpian returned invalid account JSON.", True),
        BranchDefinition(DEFECT, "The local connection credential is missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
