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

CONNECTOR_ID = "fxiaoke"
PROVIDER = "fxiaoke"
LIST_OBJECT_METADATA_OPERATION_ID = "listObjectMetadata"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_OBJECT_METADATA_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_OBJECT_METADATA_OPERATION_ID),
    (
        BranchDefinition(LISTED, "FXiaoke returned available CRM object metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "FXiaoke rejected credentials, permissions, or account quota.", True),
        BranchDefinition(INVALID_RESPONSE, "FXiaoke returned malformed or oversized metadata.", True),
        BranchDefinition(DEFECT, "Local connection settings or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
