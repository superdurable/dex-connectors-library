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

CONNECTOR_ID = "lucid"
PROVIDER = "lucid"
LIST_DOCUMENTS_OPERATION_ID = "listDocuments"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_DOCUMENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DOCUMENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Lucid returned one document page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Lucid conclusively rejected the search.", True),
        BranchDefinition(INVALID_RESPONSE, "Lucid returned malformed or oversized search data.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
