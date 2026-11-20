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

CONNECTOR_ID = "rongcloud"
PROVIDER = "rongcloud"
CHECK_USER_ONLINE_OPERATION_ID = "checkUserOnline"
CHECKED = "checked"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

CHECK_USER_ONLINE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, CHECK_USER_ONLINE_OPERATION_ID),
    (
        BranchDefinition(CHECKED, "RongCloud returned the user's online state.", False),
        BranchDefinition(PROVIDER_REJECTED, "RongCloud rejected the app credentials or user query.", True),
        BranchDefinition(INVALID_RESPONSE, "RongCloud returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
