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

CONNECTOR_ID = "google-cloud-storage"
PROVIDER = "google-cloud-storage"
LIST_OBJECTS_OPERATION_ID = "listObjects"
DOWNLOAD_OBJECT_OPERATION_ID = "downloadObject"
UPLOAD_OBJECT_OPERATION_ID = "uploadObject"
DEFECT = "defect"
DOWNLOADED = "downloaded"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
OBJECT_EXISTS = "objectExists"
PROVIDER_REJECTED = "providerRejected"
UPLOADED = "uploaded"

LIST_OBJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_OBJECTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Google returned an object page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned malformed or oversized object JSON.", True),
        BranchDefinition(DEFECT, "Local page input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

DOWNLOAD_OBJECT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, DOWNLOAD_OBJECT_OPERATION_ID),
    (
        BranchDefinition(DOWNLOADED, "Google returned the object content.", False),
        BranchDefinition(NOT_FOUND, "The object does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned oversized content or an unsafe redirect.", True),
        BranchDefinition(DEFECT, "Local object input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPLOAD_OBJECT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPLOAD_OBJECT_OPERATION_ID),
    (
        BranchDefinition(UPLOADED, "The named object now has the requested content.", False),
        BranchDefinition(OBJECT_EXISTS, "An object with different content already exists at that name.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the upload.", True),
        BranchDefinition(DEFECT, "Local upload input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
