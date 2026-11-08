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

CONNECTOR_ID = "basecamp"
PROVIDER = "basecamp"
LIST_ACCOUNTS_OPERATION_ID = "listAccounts"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_ACCOUNTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ACCOUNTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Basecamp returned accessible account identities.", False),
        BranchDefinition(PROVIDER_REJECTED, "Basecamp conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Basecamp returned malformed or oversized account JSON.", True),
        BranchDefinition(DEFECT, "Local page input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
