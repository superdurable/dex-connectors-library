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

CONNECTOR_ID = "google-bigquery"
PROVIDER = "google-bigquery"
LIST_DATASETS_OPERATION_ID = "listDatasets"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_DATASETS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DATASETS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "BigQuery returned the selected dataset page.", False),
        BranchDefinition(PROVIDER_REJECTED, "BigQuery conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "BigQuery returned malformed or oversized dataset JSON.", True),
        BranchDefinition(DEFECT, "Local project or page input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
