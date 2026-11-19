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

CONNECTOR_ID = "submail"
PROVIDER = "submail"
GET_SMS_BALANCE_OPERATION_ID = "getSMSBalance"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_SMS_BALANCE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SMS_BALANCE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "SUBMAIL returned both SMS credit balances.", False),
        BranchDefinition(PROVIDER_REJECTED, "SUBMAIL conclusively rejected the query.", True),
        BranchDefinition(INVALID_RESPONSE, "SUBMAIL returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local AppID or AppKey is missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
