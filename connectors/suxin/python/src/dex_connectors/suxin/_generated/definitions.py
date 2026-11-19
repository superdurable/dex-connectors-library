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

CONNECTOR_ID = "suxin"
PROVIDER = "suxin"
LIST_ORGANIZATIONS_OPERATION_ID = "listOrganizations"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_ORGANIZATIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ORGANIZATIONS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Suxin returned organization metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "Suxin rejected credentials or access.", True),
        BranchDefinition(INVALID_RESPONSE, "Suxin returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
