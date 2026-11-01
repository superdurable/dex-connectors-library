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

CONNECTOR_ID = "algolia"
PROVIDER = "algolia"
SEARCH_OPERATION_ID = "search"
SAVE_OBJECT_OPERATION_ID = "saveObject"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
SAVED = "saved"
SEARCHED = "searched"

SEARCH_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "Algolia returned a hit page, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Algolia rejected the search or key.", True),
        BranchDefinition(INVALID_RESPONSE, "Algolia returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SAVE_OBJECT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SAVE_OBJECT_OPERATION_ID),
    (
        BranchDefinition(SAVED, "Algolia accepted the replacement task.", False),
        BranchDefinition(PROVIDER_REJECTED, "Algolia rejected the record or key.", True),
        BranchDefinition(INVALID_RESPONSE, "Algolia returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
