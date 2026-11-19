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

CONNECTOR_ID = "quanshi"
PROVIDER = "quanshi"
LIST_ROOMS_OPERATION_ID = "listRooms"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_ROOMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ROOMS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Quanshi returned meeting room metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "Quanshi rejected credentials or room access.", True),
        BranchDefinition(INVALID_RESPONSE, "Quanshi returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
