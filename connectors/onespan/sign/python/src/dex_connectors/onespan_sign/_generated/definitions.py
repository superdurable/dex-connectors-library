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

CONNECTOR_ID = "onespan-sign"
PROVIDER = "onespan-sign"
GET_PACKAGE_OPERATION_ID = "getPackage"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_PACKAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PACKAGE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "OneSpan Sign returned the package.", False),
        BranchDefinition(NOT_FOUND, "OneSpan Sign did not expose the package.", True),
        BranchDefinition(PROVIDER_REJECTED, "OneSpan Sign conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "OneSpan Sign returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
