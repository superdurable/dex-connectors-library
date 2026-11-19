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

CONNECTOR_ID = "jingdigital"
PROVIDER = "jingdigital"
LIST_TAGS_OPERATION_ID = "listTags"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_TAGS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TAGS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "JINGdigital returned ordinary tag metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "JINGdigital rejected credentials or access.", True),
        BranchDefinition(INVALID_RESPONSE, "JINGdigital returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or pagination are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
