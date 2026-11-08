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

CONNECTOR_ID = "shortcut"
PROVIDER = "shortcut"
GET_STORY_OPERATION_ID = "getStory"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_STORY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_STORY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Shortcut returned the story.", False),
        BranchDefinition(NOT_FOUND, "Shortcut could not find the story.", True),
        BranchDefinition(PROVIDER_REJECTED, "Shortcut conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Shortcut returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
