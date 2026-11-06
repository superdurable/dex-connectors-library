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

CONNECTOR_ID = "microsoft-onedrive"
PROVIDER = "microsoft"
SEARCH_FILES_OPERATION_ID = "searchFiles"
GET_FILE_OPERATION_ID = "getFile"
READ_FILE_TEXT_OPERATION_ID = "readFileText"
UPLOAD_FILE_OPERATION_ID = "uploadFile"
CREATE_FOLDER_OPERATION_ID = "createFolder"
ALREADY_EXISTS = "alreadyExists"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NAME_CONFLICT = "nameConflict"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
READ = "read"
TOO_LARGE = "tooLarge"
UNSUPPORTED_CONTENT = "unsupportedContent"
UPLOADED = "uploaded"

SEARCH_FILES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_FILES_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Microsoft Graph returned matching items or reported that more pages remain.", False),
        BranchDefinition(NOT_FOUND, "No item matched and Microsoft Graph reported no further pages.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the search, such as a revoked grant, a missing folder, or search under Sites.Selected.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized search or listing response.", True),
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
        BranchDefinition(FOUND, "Microsoft Graph returned the item metadata.", False),
        BranchDefinition(NOT_FOUND, "The item does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the metadata read.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized metadata response.", True),
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
        BranchDefinition(UNSUPPORTED_CONTENT, "The item is a folder, a package such as a OneNote notebook, a binary or Office file, or not valid UTF-8 text.", True),
        BranchDefinition(TOO_LARGE, "The file is larger than the configured text limit.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the metadata read or content download.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized metadata response or an unusable download redirect.", True),
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
        BranchDefinition(UPLOADED, "Microsoft Graph stored the file, or the file at that path already has exactly this content, such as from an earlier attempt of the Step.", False),
        BranchDefinition(ALREADY_EXISTS, "With the fail conflict behavior, a different file or a folder already has the name, so nothing was written.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the upload, such as a missing or read-only parent folder, a blocked file type, or a full quota.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized response while reading back a conflicting item.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_FOLDER_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_FOLDER_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Microsoft Graph created the folder or a folder with that name already existed.", False),
        BranchDefinition(NAME_CONFLICT, "A file, not a folder, already has the name in the parent folder, so nothing was created.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the folder creation, such as a missing or read-only parent folder.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized response to the creation or its read-back.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
