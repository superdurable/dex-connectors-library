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

CONNECTOR_ID = "volcengine"
PROVIDER = "volcengine"
DESCRIBE_REGIONS_OPERATION_ID = "describeRegions"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

DESCRIBE_REGIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, DESCRIBE_REGIONS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "ECS returned one region page, which may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "ECS rejected the access key, permission, or query.", True),
        BranchDefinition(INVALID_RESPONSE, "ECS returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
