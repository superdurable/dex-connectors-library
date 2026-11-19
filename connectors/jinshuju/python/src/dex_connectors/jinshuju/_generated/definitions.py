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

CONNECTOR_ID = "jinshuju"
PROVIDER = "jinshuju"
LIST_FORMS_PAGE_OPERATION_ID = "listFormsPage"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_FORMS_PAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_FORMS_PAGE_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Jinshuju returned form metadata and pagination.", False),
        BranchDefinition(PROVIDER_REJECTED, "Jinshuju rejected authentication, access, or billing.", True),
        BranchDefinition(INVALID_RESPONSE, "Jinshuju returned malformed or oversized metadata.", True),
        BranchDefinition(DEFECT, "Local cursor, credentials or settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
