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

CONNECTOR_ID = "lixinger"
PROVIDER = "lixinger"
LIST_STOCK_COMPANIES_OPERATION_ID = "listStockCompanies"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_STOCK_COMPANIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_STOCK_COMPANIES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Lixinger returned company metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "Lixinger rejected token or access.", True),
        BranchDefinition(INVALID_RESPONSE, "Lixinger returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local token or stock codes are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
