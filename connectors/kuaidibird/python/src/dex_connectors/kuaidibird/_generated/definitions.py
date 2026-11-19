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

CONNECTOR_ID = "kuaidibird"
PROVIDER = "kuaidibird"
QUERY_TRACKING_OPERATION_ID = "queryTracking"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
RETRIEVED = "retrieved"

QUERY_TRACKING_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_TRACKING_OPERATION_ID),
    (
        BranchDefinition(RETRIEVED, "KuaidiBird returned a waybill state and tracking events.", False),
        BranchDefinition(PROVIDER_REJECTED, "KuaidiBird returned a definite unsuccessful query, including an unknown waybill or unavailable product.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned incomplete, malformed, or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection or input configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=30), 2, timedelta(seconds=120), 3, timedelta(seconds=300)),
    ),
)
