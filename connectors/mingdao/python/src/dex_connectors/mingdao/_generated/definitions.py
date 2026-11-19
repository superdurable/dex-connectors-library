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

CONNECTOR_ID = "mingdao"
PROVIDER = "mingdao"
LIST_WORKSHEETS_OPERATION_ID = "listWorksheets"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_WORKSHEETS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_WORKSHEETS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Mingdao returned worksheet metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "Mingdao rejected the app authorization.", True),
        BranchDefinition(INVALID_RESPONSE, "Mingdao returned malformed or oversized metadata.", True),
        BranchDefinition(DEFECT, "Local app credentials or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
