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

CONNECTOR_ID = "qingcloud"
PROVIDER = "qingcloud"
DESCRIBE_ZONES_OPERATION_ID = "describeZones"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

DESCRIBE_ZONES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, DESCRIBE_ZONES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "QingCloud returned accessible zones.", False),
        BranchDefinition(PROVIDER_REJECTED, "QingCloud conclusively rejected the query.", True),
        BranchDefinition(INVALID_RESPONSE, "QingCloud returned malformed or oversized zones JSON.", True),
        BranchDefinition(DEFECT, "The local access key is missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
