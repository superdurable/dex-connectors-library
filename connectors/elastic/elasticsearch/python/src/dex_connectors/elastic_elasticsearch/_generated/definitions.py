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

CONNECTOR_ID = "elasticsearch"
PROVIDER = "elasticsearch"
INDEX_DOCUMENT_OPERATION_ID = "indexDocument"
DELETE_DOCUMENT_OPERATION_ID = "deleteDocument"
SEARCH_DOCUMENTS_OPERATION_ID = "searchDocuments"
ABSENT = "absent"
DEFECT = "defect"
FOUND = "found"
INDEX_NOT_FOUND = "indexNotFound"
INDEXED = "indexed"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

INDEX_DOCUMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, INDEX_DOCUMENT_OPERATION_ID),
    (
        BranchDefinition(INDEXED, "Elasticsearch stored the requested document at the stable ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "Elasticsearch conclusively rejected the document replacement.", True),
        BranchDefinition(DEFECT, "Local document input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

DELETE_DOCUMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, DELETE_DOCUMENT_OPERATION_ID),
    (
        BranchDefinition(ABSENT, "The document is absent, whether deleted now or already missing.", False),
        BranchDefinition(INDEX_NOT_FOUND, "The requested index does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Elasticsearch conclusively rejected the deletion.", True),
        BranchDefinition(DEFECT, "Local document ID or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SEARCH_DOCUMENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_DOCUMENTS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Elasticsearch returned a search page, possibly empty.", False),
        BranchDefinition(INDEX_NOT_FOUND, "The requested index does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Elasticsearch conclusively rejected the search.", True),
        BranchDefinition(INVALID_RESPONSE, "Elasticsearch returned malformed or oversized search data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
