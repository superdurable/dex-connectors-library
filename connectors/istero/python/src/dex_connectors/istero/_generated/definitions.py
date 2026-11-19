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

CONNECTOR_ID = "istero"
PROVIDER = "istero"
GET_ACCOUNT_STATUS_OPERATION_ID = "getAccountStatus"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
RETRIEVED = "retrieved"

GET_ACCOUNT_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ACCOUNT_STATUS_OPERATION_ID),
    (
        BranchDefinition(RETRIEVED, "ISAS returned account quota counters.", False),
        BranchDefinition(PROVIDER_REJECTED, "ISAS rejected the token or account ID.", True),
        BranchDefinition(INVALID_RESPONSE, "The account response was malformed or oversized.", True),
        BranchDefinition(DEFECT, "Local key configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
