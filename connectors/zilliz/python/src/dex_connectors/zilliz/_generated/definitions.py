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

CONNECTOR_ID = "zilliz"
PROVIDER = "zilliz"
LIST_CLUSTERS_OPERATION_ID = "listClusters"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_CLUSTERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CLUSTERS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Zilliz returned the visible cluster page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zilliz rejected the Key or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Zilliz returned malformed or oversized cluster data.", True),
        BranchDefinition(DEFECT, "The local API Key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
