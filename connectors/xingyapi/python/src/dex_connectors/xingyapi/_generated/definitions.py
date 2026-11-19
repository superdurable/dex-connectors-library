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

CONNECTOR_ID = "xingyapi"
PROVIDER = "xingyapi"
GET_INSTANCE_ONLINE_STATUS_OPERATION_ID = "getInstanceOnlineStatus"
CHECKED = "checked"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_INSTANCE_ONLINE_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_INSTANCE_ONLINE_STATUS_OPERATION_ID),
    (
        BranchDefinition(CHECKED, "XingyAPI returned the instance login status.", False),
        BranchDefinition(PROVIDER_REJECTED, "XingyAPI rejected the Key or request.", True),
        BranchDefinition(INVALID_RESPONSE, "XingyAPI returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local Key or instance GUID is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
