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

CONNECTOR_ID = "taosdata"
PROVIDER = "taosdata"
LIST_DATABASES_OPERATION_ID = "listDatabases"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_DATABASES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DATABASES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "TDengine Cloud returned database metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "TDengine Cloud rejected authentication or the metadata query.", True),
        BranchDefinition(INVALID_RESPONSE, "TDengine Cloud returned malformed or oversized metadata.", True),
        BranchDefinition(DEFECT, "Local credentials or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
