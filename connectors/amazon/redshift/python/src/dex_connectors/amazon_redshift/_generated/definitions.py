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

CONNECTOR_ID = "amazon-redshift"
PROVIDER = "amazon-redshift"
GET_STATEMENT_RESULT_OPERATION_ID = "getStatementResult"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
MISSING = "missing"
PROVIDER_REJECTED = "providerRejected"

GET_STATEMENT_RESULT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_STATEMENT_RESULT_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "Redshift returned the statement result page.", False),
        BranchDefinition(MISSING, "Redshift does not retain the specified statement result.", True),
        BranchDefinition(PROVIDER_REJECTED, "Redshift conclusively rejected the lookup or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Redshift returned malformed or oversized result JSON.", True),
        BranchDefinition(DEFECT, "The statement ID, endpoint, Region, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
