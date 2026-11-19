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

CONNECTOR_ID = "fastgpt"
PROVIDER = "fastgpt"
GET_DATASET_DETAILS_OPERATION_ID = "getDatasetDetails"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_DATASET_DETAILS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_DATASET_DETAILS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "FastGPT returned the knowledge base metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "FastGPT rejected access or the knowledge base ID.", True),
        BranchDefinition(INVALID_RESPONSE, "FastGPT returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or knowledge base ID are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
