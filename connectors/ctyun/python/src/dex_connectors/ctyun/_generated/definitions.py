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

CONNECTOR_ID = "ctyun"
PROVIDER = "ctyun"
LIST_KNOWLEDGE_BASES_OPERATION_ID = "listKnowledgeBases"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_KNOWLEDGE_BASES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_KNOWLEDGE_BASES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "CTYun returned knowledge-base metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "CTYun rejected credentials or access.", True),
        BranchDefinition(INVALID_RESPONSE, "CTYun returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
