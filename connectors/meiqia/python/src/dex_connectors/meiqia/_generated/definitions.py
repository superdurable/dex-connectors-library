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

CONNECTOR_ID = "meiqia"
PROVIDER = "meiqia"
GET_CONVERSATION_OPERATION_ID = "getConversation"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NO_DATA = "noData"
PROVIDER_REJECTED = "providerRejected"

GET_CONVERSATION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CONVERSATION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Meiqia returned conversation metadata.", False),
        BranchDefinition(NO_DATA, "Meiqia did not find the conversation.", True),
        BranchDefinition(PROVIDER_REJECTED, "Meiqia rejected authorization, permission, or the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Meiqia returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local token or conversation ID is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
