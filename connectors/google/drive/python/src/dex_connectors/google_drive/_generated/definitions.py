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

CONNECTOR_ID = "google-drive"
PROVIDER = "google"
SEARCH_FILES_OPERATION_ID = "searchFiles"
GET_FILE_OPERATION_ID = "getFile"
READ_FILE_TEXT_OPERATION_ID = "readFileText"
UPLOAD_FILE_OPERATION_ID = "uploadFile"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
READ = "read"
TOO_LARGE = "tooLarge"
UNCERTAIN = "uncertain"
UNSUPPORTED_CONTENT = "unsupportedContent"
UPLOADED = "uploaded"

SEARCH_FILES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_FILES_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Google Drive returned matching files or reported that more pages remain.", False),
        BranchDefinition(NOT_FOUND, "No file matched and Google Drive reported no further pages.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the search, such as a revoked grant or an expired page token.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized search response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_FILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_FILE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Google Drive returned the file metadata.", False),
        BranchDefinition(NOT_FOUND, "The file does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the metadata read.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized metadata response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

READ_FILE_TEXT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, READ_FILE_TEXT_OPERATION_ID),
    (
        BranchDefinition(READ, "The file text was read within the configured limit.", False),
        BranchDefinition(NOT_FOUND, "The file does not exist or the connection cannot see it.", True),
        BranchDefinition(UNSUPPORTED_CONTENT, "The file is binary, a folder or shortcut, a Google Workspace type without a text export, or not valid UTF-8 text.", True),
        BranchDefinition(TOO_LARGE, "The file text exceeds the configured limit or Google's export limit.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the metadata read or content download.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized metadata response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
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
        BranchDefinition(UPLOADED, "Google Drive created the file or returned the file an earlier attempt created.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the upload or duplicate lookup, such as a missing or read-only parent folder.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized duplicate-lookup response before any upload was sent.", True),
        BranchDefinition(UNCERTAIN, "The dispatched upload outcome cannot be confirmed.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
