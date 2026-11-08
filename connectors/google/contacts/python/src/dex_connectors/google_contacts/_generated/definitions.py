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

CONNECTOR_ID = "google-contacts"
PROVIDER = "google-contacts"
LIST_CONTACTS_OPERATION_ID = "listContacts"
GET_CONTACT_OPERATION_ID = "getContact"
CREATE_CONTACT_OPERATION_ID = "createContact"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"

LIST_CONTACTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CONTACTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Google returned a contact page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned malformed or oversized contact JSON.", True),
        BranchDefinition(DEFECT, "Local page input or connection credentials are invalid.", True),
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
        BranchDefinition(FOUND, "Google returned the contact.", False),
        BranchDefinition(NOT_FOUND, "The contact does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned malformed or oversized contact JSON.", True),
        BranchDefinition(DEFECT, "Local resource name or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_CONTACT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_CONTACT_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Google created the contact and returned its resource name.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the contact.", True),
        BranchDefinition(UNCERTAIN, "Google may have created the contact but its resource name was not confirmed; do not resend automatically.", True),
        BranchDefinition(DEFECT, "Local contact input, connection credentials, or dispatch checkpoint is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
