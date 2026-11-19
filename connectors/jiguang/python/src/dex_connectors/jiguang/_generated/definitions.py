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

CONNECTOR_ID = "jiguang"
PROVIDER = "jiguang"
LIST_APPLICATION_TAGS_OPERATION_ID = "listApplicationTags"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_APPLICATION_TAGS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_APPLICATION_TAGS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "JPush returned the application tag names.", False),
        BranchDefinition(PROVIDER_REJECTED, "JPush conclusively rejected the query.", True),
        BranchDefinition(INVALID_RESPONSE, "JPush returned malformed or oversized tags JSON.", True),
        BranchDefinition(DEFECT, "The local AppKey or Master Secret is missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
