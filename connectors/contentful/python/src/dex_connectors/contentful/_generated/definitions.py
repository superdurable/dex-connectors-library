# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation identities, branches, and Step defaults."""

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

CONNECTOR_ID = "contentful"
PROVIDER = "contentful"
GET_ENTRY_OPERATION_ID = "getEntry"
LIST_ENTRIES_OPERATION_ID = "listEntries"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_ENTRY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ENTRY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Get one published Contentful entry.", False),
        BranchDefinition(NOT_FOUND, "The requested provider resource does not exist or is not visible.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input, configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_ENTRIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ENTRIES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List one page of published entries in a space and environment.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input, configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
