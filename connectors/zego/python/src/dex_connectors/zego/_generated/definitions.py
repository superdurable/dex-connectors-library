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

CONNECTOR_ID = "zego"
PROVIDER = "zego"
GET_ROOM_USER_COUNT_OPERATION_ID = "getRoomUserCount"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_ROOM_USER_COUNT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ROOM_USER_COUNT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "ZEGO returned the current room member count.", False),
        BranchDefinition(PROVIDER_REJECTED, "ZEGO rejected the project or room.", True),
        BranchDefinition(INVALID_RESPONSE, "ZEGO returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local project credentials or RoomId are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
