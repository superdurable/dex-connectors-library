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

CONNECTOR_ID = "siliconflow"
PROVIDER = "siliconflow"
LIST_MODELS_OPERATION_ID = "listModels"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_MODELS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MODELS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "SiliconFlow returned zero or more models.", False),
        BranchDefinition(PROVIDER_REJECTED, "SiliconFlow rejected the key or permissions.", True),
        BranchDefinition(INVALID_RESPONSE, "SiliconFlow returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Connection configuration or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
