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

CONNECTOR_ID = "agora"
PROVIDER = "agora"
LIST_RUNNING_AGENTS_OPERATION_ID = "listRunningAgents"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_RUNNING_AGENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_RUNNING_AGENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Agora returned the running agent page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Agora conclusively rejected the query.", True),
        BranchDefinition(INVALID_RESPONSE, "Agora returned malformed or oversized agents JSON.", True),
        BranchDefinition(DEFECT, "The local Agora credentials or App ID are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
