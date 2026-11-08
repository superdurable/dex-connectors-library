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

CONNECTOR_ID = "clickhouse"
PROVIDER = "clickhouse"
LIST_TABLES_OPERATION_ID = "listTables"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_TABLES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TABLES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "ClickHouse returned the table names.", False),
        BranchDefinition(PROVIDER_REJECTED, "ClickHouse conclusively rejected the metadata query.", True),
        BranchDefinition(INVALID_RESPONSE, "ClickHouse returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
