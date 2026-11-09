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

CONNECTOR_ID = "pandadoc"
PROVIDER = "pandadoc"
GET_DOCUMENT_OPERATION_ID = "getDocument"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_DOCUMENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_DOCUMENT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "PandaDoc returned the document.", False),
        BranchDefinition(NOT_FOUND, "PandaDoc did not expose the document.", True),
        BranchDefinition(PROVIDER_REJECTED, "PandaDoc conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "PandaDoc returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
