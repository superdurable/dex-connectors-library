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

CONNECTOR_ID = "gusto"
PROVIDER = "gusto"
GET_EMPLOYEE_OPERATION_ID = "getEmployee"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MISSING = "missing"
PROVIDER_REJECTED = "providerRejected"

GET_EMPLOYEE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_EMPLOYEE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Gusto returned the selected employee.", False),
        BranchDefinition(MISSING, "Gusto returned no employee for this UUID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Gusto conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Gusto returned malformed or oversized employee JSON.", True),
        BranchDefinition(DEFECT, "The employee UUID or OAuth credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
