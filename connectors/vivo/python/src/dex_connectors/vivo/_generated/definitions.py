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

CONNECTOR_ID = "vivo"
PROVIDER = "vivo"
GET_ONLINE_VERSION_OPERATION_ID = "getOnlineVersion"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NO_DATA = "noData"
PROVIDER_REJECTED = "providerRejected"

GET_ONLINE_VERSION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ONLINE_VERSION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "vivo returned the online app version.", False),
        BranchDefinition(NO_DATA, "vivo has no online app for this package.", True),
        BranchDefinition(PROVIDER_REJECTED, "vivo rejected the credentials or request.", True),
        BranchDefinition(INVALID_RESPONSE, "vivo returned malformed or oversized version data.", True),
        BranchDefinition(DEFECT, "Local credentials or package name are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
