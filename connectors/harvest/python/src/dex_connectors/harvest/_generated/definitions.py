# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation identities, branches, and Step defaults."""

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

CONNECTOR_ID = "harvest"
PROVIDER = "harvest"
LIST_PROJECTS_OPERATION_ID = "listProjects"
LIST_TIME_ENTRIES_OPERATION_ID = "listTimeEntries"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_PROJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PROJECTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Harvest returned the requested data.", False),
        BranchDefinition(PROVIDER_REJECTED, "Harvest conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Harvest returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_TIME_ENTRIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TIME_ENTRIES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Harvest returned the requested data.", False),
        BranchDefinition(PROVIDER_REJECTED, "Harvest conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Harvest returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
