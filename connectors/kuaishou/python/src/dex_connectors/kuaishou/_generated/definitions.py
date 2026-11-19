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

CONNECTOR_ID = "kuaishou"
PROVIDER = "kuaishou"
LIST_USER_VIDEOS_OPERATION_ID = "listUserVideos"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_USER_VIDEOS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_USER_VIDEOS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Kuaishou returned one video page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Kuaishou conclusively rejected the list query.", True),
        BranchDefinition(INVALID_RESPONSE, "Kuaishou returned malformed or oversized video JSON.", True),
        BranchDefinition(DEFECT, "The local credentials, page size, or cursor are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
