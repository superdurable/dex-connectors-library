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

CONNECTOR_ID = "kingsoftoffice"
PROVIDER = "kingsoftoffice"
LIST_FOLDER_FILES_OPERATION_ID = "listFolderFiles"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_FOLDER_FILES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_FOLDER_FILES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "WPS returned one file metadata page.", False),
        BranchDefinition(PROVIDER_REJECTED, "WPS rejected credentials, scope, drive, folder, or page token.", True),
        BranchDefinition(INVALID_RESPONSE, "WPS returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
