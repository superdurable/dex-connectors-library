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

CONNECTOR_ID = "datadog"
PROVIDER = "datadog"
QUERY_METRICS_OPERATION_ID = "queryMetrics"
LIST_MONITORS_OPERATION_ID = "listMonitors"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"
QUERIED = "queried"

QUERY_METRICS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_METRICS_OPERATION_ID),
    (
        BranchDefinition(QUERIED, "Query Datadog timeseries points over an explicit Unix-second interval.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_MONITORS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MONITORS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List one page of Datadog monitors.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
