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

CONNECTOR_ID = "campaignmonitor"
PROVIDER = "campaignmonitor"
LIST_CLIENTS_OPERATION_ID = "listClients"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_CLIENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CLIENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Campaign Monitor returned the account's clients.", False),
        BranchDefinition(PROVIDER_REJECTED, "Campaign Monitor conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Campaign Monitor returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
