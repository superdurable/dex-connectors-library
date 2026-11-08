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

CONNECTOR_ID = "freshsales"
PROVIDER = "freshsales"
LIST_CONTACT_VIEWS_OPERATION_ID = "listContactViews"
GET_CONTACT_OPERATION_ID = "getContact"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_CONTACT_VIEWS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CONTACT_VIEWS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Freshsales returned the available contact views.", False),
        BranchDefinition(PROVIDER_REJECTED, "Freshsales conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Freshsales returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Unavailable or invalid connection credentials.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_CONTACT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CONTACT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Freshsales returned the selected contact.", False),
        BranchDefinition(NOT_FOUND, "No contact matches this ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Freshsales conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Freshsales returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Invalid contact ID or unavailable credentials.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
