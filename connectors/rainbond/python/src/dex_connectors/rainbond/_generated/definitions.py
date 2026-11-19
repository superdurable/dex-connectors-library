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

CONNECTOR_ID = "rainbond"
PROVIDER = "rainbond"
LIST_TEAMS_OPERATION_ID = "listTeams"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_TEAMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TEAMS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Rainbond returned one team page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Rainbond rejected the token or access.", True),
        BranchDefinition(INVALID_RESPONSE, "Rainbond returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or pagination options are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
