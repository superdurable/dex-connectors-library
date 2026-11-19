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

CONNECTOR_ID = "kuaidi100"
PROVIDER = "kuaidi100"
QUERY_TRACKING_OPERATION_ID = "queryTracking"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
RETRIEVED = "retrieved"

QUERY_TRACKING_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_TRACKING_OPERATION_ID),
    (
        BranchDefinition(RETRIEVED, "The carrier returned tracking state and events.", False),
        BranchDefinition(PROVIDER_REJECTED, "Kuaidi100 definitively rejected or could not find this waybill.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned an invalid or oversized response.", True),
        BranchDefinition(DEFECT, "Local connection or request configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1800), 1, timedelta(seconds=1800), 2, timedelta(seconds=3660)),
    ),
)
