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

CONNECTOR_ID = "amazon-sns"
PROVIDER = "amazon-sns"
LIST_TOPICS_OPERATION_ID = "listTopics"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PAGE = "page"
PROVIDER_REJECTED = "providerRejected"

LIST_TOPICS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TOPICS_OPERATION_ID),
    (
        BranchDefinition(PAGE, "AWS returned one topic page, which may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "AWS conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "AWS returned malformed or oversized topic XML.", True),
        BranchDefinition(DEFECT, "The pagination token, endpoint, Region, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
