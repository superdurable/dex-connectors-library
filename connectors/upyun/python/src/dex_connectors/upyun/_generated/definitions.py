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

CONNECTOR_ID = "upyun"
PROVIDER = "upyun"
GET_SERVICE_USAGE_OPERATION_ID = "getServiceUsage"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
READ = "read"

GET_SERVICE_USAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SERVICE_USAGE_OPERATION_ID),
    (
        BranchDefinition(READ, "UPYUN returned the service usage in bytes.", False),
        BranchDefinition(PROVIDER_REJECTED, "UPYUN conclusively rejected the usage query.", True),
        BranchDefinition(INVALID_RESPONSE, "UPYUN returned malformed or oversized usage text.", True),
        BranchDefinition(DEFECT, "Local bucket or operator credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
