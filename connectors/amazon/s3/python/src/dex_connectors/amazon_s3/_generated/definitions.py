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

CONNECTOR_ID = "amazon-s3"
PROVIDER = "amazon-s3"
LIST_OBJECTS_OPERATION_ID = "listObjects"
HEAD_OBJECT_OPERATION_ID = "headObject"
GET_OBJECT_TEXT_OPERATION_ID = "getObjectText"
PUT_OBJECT_OPERATION_ID = "putObject"
ALREADY_EXISTS = "alreadyExists"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
READ = "read"
STORED = "stored"
TOO_LARGE = "tooLarge"
UNSUPPORTED_CONTENT = "unsupportedContent"

LIST_OBJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_OBJECTS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The page holds objects or common prefixes, or more pages remain.", False),
        BranchDefinition(NOT_FOUND, "No object matched the prefix and no further page remains.", True),
        BranchDefinition(PROVIDER_REJECTED, "The store conclusively rejected the list, such as a missing bucket, a wrong Region, invalid credentials, or missing s3:ListBucket permission.", True),
        BranchDefinition(INVALID_RESPONSE, "The store returned an invalid or oversized list page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

HEAD_OBJECT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, HEAD_OBJECT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The store returned the object's metadata.", False),
        BranchDefinition(NOT_FOUND, "The object, or its bucket, does not exist; a HEAD response cannot say which.", True),
        BranchDefinition(PROVIDER_REJECTED, "The store conclusively rejected the read, such as a wrong Region, invalid credentials, or missing s3:GetObject permission.", True),
        BranchDefinition(INVALID_RESPONSE, "The store returned metadata without a valid size or modification time.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_OBJECT_TEXT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_OBJECT_TEXT_OPERATION_ID),
    (
        BranchDefinition(READ, "The object's complete text was read within maxTextBytes.", False),
        BranchDefinition(NOT_FOUND, "The object does not exist.", True),
        BranchDefinition(UNSUPPORTED_CONTENT, "The object's content type is not text, its content is encoded such as gzip, or its bytes are not valid UTF-8 text.", True),
        BranchDefinition(TOO_LARGE, "The object is larger than maxTextBytes.", True),
        BranchDefinition(PROVIDER_REJECTED, "The store conclusively rejected the read, such as a missing bucket, a wrong Region, invalid credentials, or missing s3:GetObject permission.", True),
        BranchDefinition(INVALID_RESPONSE, "The store returned a response without a valid size or with content that contains the connection's credentials.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

PUT_OBJECT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, PUT_OBJECT_OPERATION_ID),
    (
        BranchDefinition(STORED, "The store holds the object this Step execution wrote.", False),
        BranchDefinition(ALREADY_EXISTS, "Create-only mode found an object at the key that this Step execution did not write; nothing was written.", True),
        BranchDefinition(PROVIDER_REJECTED, "The store conclusively rejected the write, such as a missing bucket, a wrong Region, invalid credentials, missing s3:PutObject permission, or a store that does not support create-only writes.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
