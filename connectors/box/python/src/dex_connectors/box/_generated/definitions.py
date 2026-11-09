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

CONNECTOR_ID = "box"
PROVIDER = "box"
DOWNLOAD_FILE_OPERATION_ID = "downloadFile"
UPLOAD_FILE_OPERATION_ID = "uploadFile"
DEFECT = "defect"
DOWNLOADED = "downloaded"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
TOO_LARGE = "tooLarge"
UPLOADED = "uploaded"

DOWNLOAD_FILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, DOWNLOAD_FILE_OPERATION_ID),
    (
        BranchDefinition(DOWNLOADED, "Box returned the selected file content.", False),
        BranchDefinition(NOT_FOUND, "Box did not find this file or the user lacks access.", True),
        BranchDefinition(TOO_LARGE, "The file exceeds the configured download limit.", True),
        BranchDefinition(PROVIDER_REJECTED, "Box conclusively rejected the download.", True),
        BranchDefinition(INVALID_RESPONSE, "Box returned an invalid or oversized response.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPLOAD_FILE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPLOAD_FILE_OPERATION_ID),
    (
        BranchDefinition(UPLOADED, "The existing Box file now has the requested content.", False),
        BranchDefinition(NOT_FOUND, "Box did not find the file or the user lacks access.", True),
        BranchDefinition(PROVIDER_REJECTED, "Box conclusively rejected the replacement.", True),
        BranchDefinition(INVALID_RESPONSE, "Box returned malformed or oversized file metadata.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
