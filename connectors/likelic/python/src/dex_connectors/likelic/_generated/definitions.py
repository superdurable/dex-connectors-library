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

CONNECTOR_ID = "likelic"
PROVIDER = "likelic"
LIST_ROOMS_OPERATION_ID = "listRooms"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_ROOMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ROOMS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Likelic returned a room page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Likelic rejected authorization, permission, or the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Likelic returned malformed or oversized room data.", True),
        BranchDefinition(DEFECT, "The local credentials or page are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
