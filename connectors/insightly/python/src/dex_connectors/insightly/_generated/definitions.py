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

CONNECTOR_ID = "insightly"
PROVIDER = "insightly"
LIST_CONTACTS_OPERATION_ID = "listContacts"
GET_CONTACT_OPERATION_ID = "getContact"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_CONTACTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CONTACTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Insightly returned the requested contact page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Insightly conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Insightly returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Invalid page input or connection credentials.", True),
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
        BranchDefinition(FOUND, "Insightly returned the selected contact.", False),
        BranchDefinition(NOT_FOUND, "No contact matches this ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Insightly conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Insightly returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Invalid contact ID or unavailable credentials.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
