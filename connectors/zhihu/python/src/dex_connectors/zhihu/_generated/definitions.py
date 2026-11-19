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

CONNECTOR_ID = "zhihu"
PROVIDER = "zhihu"
LIST_QUOTAS_OPERATION_ID = "listQuotas"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_QUOTAS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_QUOTAS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Zhihu returned quota categories, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zhihu rejected the credential or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Zhihu returned malformed or oversized quota data.", True),
        BranchDefinition(DEFECT, "Local connection configuration or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
