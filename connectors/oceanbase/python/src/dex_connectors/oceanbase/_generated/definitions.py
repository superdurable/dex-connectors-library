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

CONNECTOR_ID = "oceanbase"
PROVIDER = "oceanbase"
LIST_INSTANCES_OPERATION_ID = "listInstances"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_INSTANCES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_INSTANCES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "OceanBase Cloud returned a page of instances.", False),
        BranchDefinition(PROVIDER_REJECTED, "OceanBase Cloud rejected authentication, permission, or request.", True),
        BranchDefinition(INVALID_RESPONSE, "OceanBase Cloud returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
