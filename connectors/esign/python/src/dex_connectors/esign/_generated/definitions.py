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

CONNECTOR_ID = "esign"
PROVIDER = "esign"
GET_SIGN_FLOW_STATUS_OPERATION_ID = "getSignFlowStatus"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_SIGN_FLOW_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SIGN_FLOW_STATUS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "eSign returned the flow status.", False),
        BranchDefinition(PROVIDER_REJECTED, "eSign rejected authentication or access.", True),
        BranchDefinition(INVALID_RESPONSE, "eSign returned malformed or oversized details.", True),
        BranchDefinition(DEFECT, "Local flow ID or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
