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

CONNECTOR_ID = "microsoft-azure-blob"
PROVIDER = "microsoft-azure-blob"
GET_BLOB_OPERATION_ID = "getBlob"
DEFECT = "defect"
DOWNLOADED = "downloaded"
INVALID_RESPONSE = "invalidResponse"
MISSING = "missing"
PROVIDER_REJECTED = "providerRejected"

GET_BLOB_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_BLOB_OPERATION_ID),
    (
        BranchDefinition(DOWNLOADED, "Azure returned the complete blob.", False),
        BranchDefinition(MISSING, "The container or blob does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Azure conclusively rejected the download or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Azure returned an oversized body or invalid response metadata.", True),
        BranchDefinition(DEFECT, "The account, container, blob path, endpoint, or key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
