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

CONNECTOR_ID = "google-docs"
PROVIDER = "google"
GET_DOCUMENT_TEXT_OPERATION_ID = "getDocumentText"
CREATE_DOCUMENT_OPERATION_ID = "createDocument"
REPLACE_DOCUMENT_TEXT_OPERATION_ID = "replaceDocumentText"
APPEND_TEXT_OPERATION_ID = "appendText"
APPENDED = "appended"
CREATED = "created"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PLACEHOLDER_NOT_FOUND = "placeholderNotFound"
PROVIDER_REJECTED = "providerRejected"
READ = "read"
REPLACED = "replaced"
REVISION_CHANGED = "revisionChanged"
TOO_LARGE = "tooLarge"
UNCERTAIN = "uncertain"

GET_DOCUMENT_TEXT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_DOCUMENT_TEXT_OPERATION_ID),
    (
        BranchDefinition(READ, "The document text was rendered within the configured limit.", False),
        BranchDefinition(NOT_FOUND, "The document does not exist or the connection cannot see it.", True),
        BranchDefinition(TOO_LARGE, "The document response or its rendered text exceeds the configured limit.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the read, such as a revoked grant or a missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid document response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_DOCUMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_DOCUMENT_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Google created the document or returned the one an earlier attempt of the same Step execution created.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the creation or the duplicate lookup, such as a missing parent folder; no document was created.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized duplicate-lookup response before the creation was sent.", True),
        BranchDefinition(UNCERTAIN, "A creation may have been sent but its outcome cannot be confirmed, so the document may exist; the connector sends nothing after selecting this branch.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)

REPLACE_DOCUMENT_TEXT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, REPLACE_DOCUMENT_TEXT_OPERATION_ID),
    (
        BranchDefinition(REPLACED, "The document holds the requested text, written by this attempt or an earlier attempt of the same Step execution.", False),
        BranchDefinition(PLACEHOLDER_NOT_FOUND, "A requested placeholder does not occur in the body at the required revision; nothing was written.", True),
        BranchDefinition(REVISION_CHANGED, "The document changed after the required revision and does not hold the requested text; nothing was written.", True),
        BranchDefinition(NOT_FOUND, "The document does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the change, such as a connection without edit access.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized document response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

APPEND_TEXT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, APPEND_TEXT_OPERATION_ID),
    (
        BranchDefinition(APPENDED, "The document ends with the requested text, appended by this attempt or an earlier attempt of the same Step execution.", False),
        BranchDefinition(REVISION_CHANGED, "The document changed after the required revision and does not end with the requested text; nothing was written.", True),
        BranchDefinition(NOT_FOUND, "The document does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the change, such as a connection without edit access.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized document response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
