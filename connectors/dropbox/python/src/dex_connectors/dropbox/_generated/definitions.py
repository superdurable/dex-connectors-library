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

CONNECTOR_ID = "dropbox"
PROVIDER = "dropbox"
LIST_FOLDER_OPERATION_ID = "listFolder"
GET_METADATA_OPERATION_ID = "getMetadata"
CONTINUE_FOLDER_OPERATION_ID = "continueFolder"
DOWNLOAD_FILE_OPERATION_ID = "downloadFile"
UPLOAD_FILE_OPERATION_ID = "uploadFile"
CURSOR_EXPIRED = "cursorExpired"
DEFECT = "defect"
DOWNLOADED = "downloaded"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
TOO_LARGE = "tooLarge"
UPLOADED = "uploaded"

LIST_FOLDER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_FOLDER_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Dropbox returned a folder page.", False),
        BranchDefinition(NOT_FOUND, "Dropbox did not find this folder.", True),
        BranchDefinition(PROVIDER_REJECTED, "Dropbox conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Dropbox returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_METADATA_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_METADATA_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Dropbox returned the selected entry.", False),
        BranchDefinition(NOT_FOUND, "Dropbox did not find this path or ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Dropbox conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Dropbox returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CONTINUE_FOLDER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, CONTINUE_FOLDER_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Dropbox returned another folder page or change set.", False),
        BranchDefinition(CURSOR_EXPIRED, "Dropbox requires a new listFolder call to reset this cursor.", True),
        BranchDefinition(PROVIDER_REJECTED, "Dropbox conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Dropbox returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local cursor or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

DOWNLOAD_FILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, DOWNLOAD_FILE_OPERATION_ID),
    (
        BranchDefinition(DOWNLOADED, "Dropbox returned the selected file content.", False),
        BranchDefinition(NOT_FOUND, "Dropbox did not find this file.", True),
        BranchDefinition(TOO_LARGE, "The file exceeds the configured download limit.", True),
        BranchDefinition(PROVIDER_REJECTED, "Dropbox conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Dropbox returned missing or malformed file metadata.", True),
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
        BranchDefinition(UPLOADED, "Dropbox stored the replacement file content.", False),
        BranchDefinition(PROVIDER_REJECTED, "Dropbox conclusively rejected the upload.", True),
        BranchDefinition(INVALID_RESPONSE, "Dropbox returned malformed or oversized upload metadata.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
