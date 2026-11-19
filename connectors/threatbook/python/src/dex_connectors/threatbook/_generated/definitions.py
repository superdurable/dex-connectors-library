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

CONNECTOR_ID = "threatbook"
PROVIDER = "threatbook"
CHECK_IP_REPUTATION_OPERATION_ID = "checkIPReputation"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NO_DATA = "noData"
PROVIDER_REJECTED = "providerRejected"

CHECK_IP_REPUTATION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, CHECK_IP_REPUTATION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "ThreatBook returned an IP risk verdict.", False),
        BranchDefinition(NO_DATA, "ThreatBook has no intelligence for this IP.", True),
        BranchDefinition(PROVIDER_REJECTED, "ThreatBook rejected the Key or query.", True),
        BranchDefinition(INVALID_RESPONSE, "ThreatBook returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local Key or IP address is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
