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

CONNECTOR_ID = "dropbox-sign"
PROVIDER = "dropbox-sign"
GET_SIGNATURE_REQUEST_OPERATION_ID = "getSignatureRequest"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_SIGNATURE_REQUEST_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SIGNATURE_REQUEST_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Dropbox Sign returned the signature request.", False),
        BranchDefinition(NOT_FOUND, "Dropbox Sign did not expose the request.", True),
        BranchDefinition(PROVIDER_REJECTED, "Dropbox Sign conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Dropbox Sign returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
