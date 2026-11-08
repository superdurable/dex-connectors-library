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

CONNECTOR_ID = "miro"
PROVIDER = "miro"
GET_BOARD_OPERATION_ID = "getBoard"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_BOARD_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_BOARD_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Miro returned the board.", False),
        BranchDefinition(NOT_FOUND, "Miro did not expose the board.", True),
        BranchDefinition(PROVIDER_REJECTED, "Miro conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Miro returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
