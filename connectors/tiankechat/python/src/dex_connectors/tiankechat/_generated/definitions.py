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

CONNECTOR_ID = "tiankechat"
PROVIDER = "tiankechat"
LIST_KNOWLEDGE_ENTRIES_OPERATION_ID = "listKnowledgeEntries"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_KNOWLEDGE_ENTRIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_KNOWLEDGE_ENTRIES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "TiankeChat returned a knowledge page.", False),
        BranchDefinition(PROVIDER_REJECTED, "TiankeChat rejected credentials or application access.", True),
        BranchDefinition(INVALID_RESPONSE, "TiankeChat returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or page options are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
