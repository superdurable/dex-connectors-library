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

CONNECTOR_ID = "alibaba"
PROVIDER = "alibaba"
DESCRIBE_REGIONS_OPERATION_ID = "describeRegions"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

DESCRIBE_REGIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, DESCRIBE_REGIONS_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "Alibaba Cloud returned its ECS regions.", False),
        BranchDefinition(PROVIDER_REJECTED, "Alibaba Cloud rejected the credentials or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Alibaba Cloud returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
