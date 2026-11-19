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

CONNECTOR_ID = "ones"
PROVIDER = "ones"
LIST_TEAMS_OPERATION_ID = "listTeams"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_TEAMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TEAMS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "ONES returned accessible teams.", False),
        BranchDefinition(PROVIDER_REJECTED, "ONES rejected the key or permissions.", True),
        BranchDefinition(INVALID_RESPONSE, "ONES returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or tenant configuration are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
