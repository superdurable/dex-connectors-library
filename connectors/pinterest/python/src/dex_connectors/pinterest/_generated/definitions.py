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

CONNECTOR_ID = "pinterest"
PROVIDER = "pinterest"
LIST_PINS_OPERATION_ID = "listPins"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_PINS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PINS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Pinterest returned the selected Pin page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Pinterest conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Pinterest returned malformed or oversized Pin JSON.", True),
        BranchDefinition(DEFECT, "Local page input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
