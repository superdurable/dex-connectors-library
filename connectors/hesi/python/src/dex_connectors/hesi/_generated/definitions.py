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

CONNECTOR_ID = "hesi"
PROVIDER = "hesi"
LIST_DEPARTMENTS_OPERATION_ID = "listDepartments"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_DEPARTMENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DEPARTMENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Hesi returned department metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "Hesi rejected credentials or access.", True),
        BranchDefinition(INVALID_RESPONSE, "Hesi returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or pagination are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
