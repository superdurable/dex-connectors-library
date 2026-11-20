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

CONNECTOR_ID = "qiyuesuo"
PROVIDER = "qiyuesuo"
LIST_CONTRACTS_OPERATION_ID = "listContracts"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_CONTRACTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CONTRACTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Qiyuesuo returned contract statuses.", False),
        BranchDefinition(PROVIDER_REJECTED, "Qiyuesuo rejected the token, signature, or access.", True),
        BranchDefinition(INVALID_RESPONSE, "Qiyuesuo returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or pagination are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
