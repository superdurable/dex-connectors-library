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

CONNECTOR_ID = "emq"
PROVIDER = "emq"
LIST_DEPLOYMENTS_OPERATION_ID = "listDeployments"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_DEPLOYMENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DEPLOYMENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "EMQX Cloud returned zero or more deployments.", False),
        BranchDefinition(PROVIDER_REJECTED, "EMQX Cloud rejected the key or request.", True),
        BranchDefinition(INVALID_RESPONSE, "EMQX Cloud returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
