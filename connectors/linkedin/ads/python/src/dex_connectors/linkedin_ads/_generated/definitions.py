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

CONNECTOR_ID = "linkedin-ads"
PROVIDER = "linkedin-ads"
LIST_AD_ACCOUNTS_OPERATION_ID = "listAdAccounts"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_AD_ACCOUNTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_AD_ACCOUNTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "LinkedIn returned the selected ad account page.", False),
        BranchDefinition(PROVIDER_REJECTED, "LinkedIn conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "LinkedIn returned malformed or oversized account JSON.", True),
        BranchDefinition(DEFECT, "Page input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
