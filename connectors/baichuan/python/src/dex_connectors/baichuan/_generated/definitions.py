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

CONNECTOR_ID = "baichuan"
PROVIDER = "baichuan"
LIST_KNOWLEDGE_BASES_OPERATION_ID = "listKnowledgeBases"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_KNOWLEDGE_BASES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_KNOWLEDGE_BASES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Baichuan returned zero or more knowledge bases.", False),
        BranchDefinition(PROVIDER_REJECTED, "Baichuan rejected the key or account permissions.", True),
        BranchDefinition(INVALID_RESPONSE, "Baichuan returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Connection configuration or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
