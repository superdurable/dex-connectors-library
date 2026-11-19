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

CONNECTOR_ID = "lingxing"
PROVIDER = "lingxing"
LIST_STORES_OPERATION_ID = "listStores"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_STORES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_STORES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Lingxing returned authorized Amazon stores, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Lingxing rejected credentials, allowlist, plan, or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Lingxing returned malformed or oversized token/store data.", True),
        BranchDefinition(DEFECT, "Local AppID or AppSecret is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
