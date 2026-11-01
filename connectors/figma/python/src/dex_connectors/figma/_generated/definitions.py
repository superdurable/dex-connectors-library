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

CONNECTOR_ID = "figma"
PROVIDER = "figma"
GET_FILE_OPERATION_ID = "getFile"
GET_FILE_COMMENTS_OPERATION_ID = "getFileComments"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_FILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_FILE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Figma returned the requested data.", False),
        BranchDefinition(NOT_FOUND, "Figma does not have the requested resource.", True),
        BranchDefinition(PROVIDER_REJECTED, "Figma conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Figma returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_FILE_COMMENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_FILE_COMMENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Figma returned the requested data.", False),
        BranchDefinition(NOT_FOUND, "Figma does not have the requested resource.", True),
        BranchDefinition(PROVIDER_REJECTED, "Figma conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Figma returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=45),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
