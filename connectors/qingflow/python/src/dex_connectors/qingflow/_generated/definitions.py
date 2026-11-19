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

CONNECTOR_ID = "qingflow"
PROVIDER = "qingflow"
LIST_APPLICATIONS_OPERATION_ID = "listApplications"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_APPLICATIONS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_APPLICATIONS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Qingflow returned workspace application metadata.", False),
        BranchDefinition(PROVIDER_REJECTED, "Qingflow rejected authentication or access.", True),
        BranchDefinition(INVALID_RESPONSE, "Qingflow returned malformed or oversized metadata.", True),
        BranchDefinition(DEFECT, "Local accessToken is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
