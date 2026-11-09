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

CONNECTOR_ID = "mongodb"
PROVIDER = "mongodb"
FIND_DOCUMENTS_OPERATION_ID = "findDocuments"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
TRUNCATED = "truncated"

FIND_DOCUMENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_DOCUMENTS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "MongoDB returned all matching documents within the configured bounds.", False),
        BranchDefinition(TRUNCATED, "More documents or bytes were available than configured bounds permit.", True),
        BranchDefinition(PROVIDER_REJECTED, "MongoDB conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "MongoDB returned a document the connector could not encode safely.", True),
        BranchDefinition(DEFECT, "Collection, filter, connection URI, or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
