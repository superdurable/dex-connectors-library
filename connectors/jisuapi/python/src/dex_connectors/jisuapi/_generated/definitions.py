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

CONNECTOR_ID = "jisuapi"
PROVIDER = "jisuapi"
LIST_PROVINCES_OPERATION_ID = "listProvinces"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_PROVINCES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PROVINCES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "JisuAPI returned province identifiers and names.", False),
        BranchDefinition(PROVIDER_REJECTED, "JisuAPI rejected the key or access.", True),
        BranchDefinition(INVALID_RESPONSE, "JisuAPI returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local appkey is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
